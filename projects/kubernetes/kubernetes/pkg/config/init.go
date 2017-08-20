// initialise a kubernetes service running in a CM
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/linuxkit/linuxkit/projects/kubernetes/kubernetes/pkg/common"
	"github.com/moby/vpnkit/go/pkg/vpnkit"
)

// Configuration is the requested initial kubernetes configuration
type Configuration struct {
	NodeName        string // name in the TLS certificate
	Version         string // kubernetes version
	ExternalPort    uint16 // API server port on the host
	HyperkitConnect string // path to the `connect` socket
	VpnkitControl   string // path to the vpnkit port control socket
	SetupPort       int    // AF_VSOCK port of the setup server
	httpc           *http.Client
}

// Rewrite the server: in the .kube/config to point to localhost rather than an
// internal IP e.g. instead of
//   server: https://192.168.65.3:6443
// we should have
//   server: https://localhost:6443
func rewriteKubeConf(original string) string {
	// FIXME(djs55): localhost is hardcoded here
	return regexp.MustCompile(`server: https://[\d\.]+:6443`).ReplaceAllString(original, "server: https://localhost:6443")
}

// httpClient returns an http client that can talk over the Unix domain socket to
// the AF_VSOCK server
func (c *Configuration) httpClient() http.Client {
	// the httpClient is a singleton
	if c.httpc != nil {
		return *c.httpc
	}
	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				conn, err := d.DialContext(ctx, "unix", c.HyperkitConnect)
				if err != nil {
					return conn, err
				}
				// request the setup service
				remote := strings.NewReader(fmt.Sprintf("00000003.%08x\n", c.SetupPort))
				_, err = io.Copy(conn, remote)
				if err != nil {
					log.Fatalf("Failed to write AF_VSOCK address %x to %s: %s", c.SetupPort, c.HyperkitConnect, err)
				}
				return conn, err
			},
		},
	}
	c.httpc = &client
	return client
}

// applyDefaults fills in defaults for any fields not supplied by the user
func (c *Configuration) applyDefaults() {
	defaultNodeName := "localhost"
	defaultVersion := "v1.7.2"
	defaultPort := uint16(6443)
	defaultHyperkitConnect := os.Getenv("HOME") + "/Library/Containers/com.docker.docker/Data/connect"
	defaultVpnkitControl := os.Getenv("HOME") + "/Library/Containers/com.docker.docker/Data/s51"
	if c.NodeName == "" {
		c.NodeName = defaultNodeName
	}
	if c.Version == "" {
		c.Version = defaultVersion
	}
	if c.ExternalPort == 0 {
		c.ExternalPort = defaultPort
	}
	if c.HyperkitConnect == "" {
		c.HyperkitConnect = defaultHyperkitConnect
	}
	if c.VpnkitControl == "" {
		c.VpnkitControl = defaultVpnkitControl
	}
	if c.SetupPort == 0 {
		c.SetupPort = 0xf3a3
	}
}

// initKubernetes initialises the cluster, starts the services and writes the
// .kube/config file on the host.
func (c *Configuration) initKubernetes() error {
	httpc := c.httpClient()
	// The NodeName will end up in the SSL certificate and must match the config file
	req := common.InitRequest{NodeName: c.NodeName, Version: c.Version}
	reqBody := new(bytes.Buffer)
	json.NewEncoder(reqBody).Encode(req)
	response, err := httpc.Post("http://unix"+common.Init, "application/json", reqBody)
	if err != nil {
		log.Printf("Failed to invoke init: %s", err)
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		msg, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Printf("Failed to read error message from init: %s", err)
			return err
		}
		log.Printf("Init failed with: %s", msg)
		return errors.New(string(msg))
	}
	var res common.InitResponse
	err = json.NewDecoder(response.Body).Decode(&res)
	if err != nil {
		log.Printf("Failed to parse result of init: %s", err)
		return err
	}
	log.Printf("Init returned %v\n", res)
	conf := rewriteKubeConf(res.AdminConf)
	kubeDir := filepath.Join(os.Getenv("HOME"), ".kube")
	if err = os.MkdirAll(kubeDir, os.ModePerm); err != nil {
		log.Printf("Failed to create %s: %s", kubeDir, err)
		return err
	}
	configPath := filepath.Join(kubeDir, "config")
	if err = ioutil.WriteFile(configPath, []byte(conf), 0644); err != nil {
		log.Printf("Failed to write %s: %s", configPath, err)
		return err
	}
	return nil
}

// getPrivateIP returns the address used by the VM on the vpnkit network,
// suitable for using as a port forward destination.
func (c *Configuration) getPrivateIP() (string, error) {
	httpc := c.httpClient()
	response, err := httpc.Get("http://unix" + common.GetIP)
	if err != nil {
		log.Printf("Failed to invoke get_ip: %s", err)
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		msg, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Printf("Failed to read error message from get_ip: %s", err)
			return "", err
		}
		log.Printf("get_ip failed with: %s", msg)
		return "", errors.New(string(msg))
	}
	var res common.GetIPResponse
	err = json.NewDecoder(response.Body).Decode(&res)
	if err != nil {
		log.Printf("Failed to parse result of get_ip: %s", err)
		return "", err
	}
	log.Printf("get_ip returned %v\n", res)
	return res.IP, nil
}

// exposePort creates a vpnkit port forward, clearing any existing user of
// the external port. The `privateIP` refers to the IP on the vpnkit network
// used by the VM.
func (c *Configuration) exposePort(privateIP string) error {
	ctx := context.TODO()
	vpn, err := vpnkit.NewConnection(ctx, c.VpnkitControl)
	if err != nil {
		log.Printf("Failed to connect to vpnkit on %s: %s", c.VpnkitControl, err)
		return err
	}
	proto := "tcp"
	outIP := net.ParseIP("0.0.0.0")
	outPort := int16(c.ExternalPort)
	inIP := net.ParseIP(privateIP)
	inPort := int16(6443)
	// clear any existing port forward
	current, err := vpnkit.ListExposed(vpn)
	if err != nil {
		log.Printf("Failed to list current port forwards: %s", err)
		return err
	}
	for _, p := range current {
		if p.OutPort() == outPort {
			log.Printf("Clearing existing port forward: %s", p.String())
			if err = p.Unexpose(ctx); err != nil {
				log.Printf("Failed to clear existing port forward: %s", err)
				return err
			}
		}
	}

	p := vpnkit.NewPort(vpn, proto, outIP, outPort, inIP, inPort)
	if err = p.Expose(ctx); err != nil {
		log.Printf("Failed to create port forward: %s", err)
		return err
	}
	return nil
}

// Setup connects to the VM and performs the initial setup, retrieving
// and installing the config file.
func (c *Configuration) Setup() error {
	c.applyDefaults()

	if err := c.initKubernetes(); err != nil {
		log.Printf("Failed to initialise the cluster: %s", err)
		return err
	}
	privateIP, err := c.getPrivateIP()
	if err != nil {
		log.Printf("Failed to query the private IP address: %s", err)
		return err
	}
	if err = c.exposePort(privateIP); err != nil {
		log.Printf("Failed to expose the external port: %s", err)
		return err
	}
	return nil
}
