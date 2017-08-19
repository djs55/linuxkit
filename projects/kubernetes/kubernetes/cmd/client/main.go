package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
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
	vpnkit "github.com/moby/vpnkit/go/pkg/vpnkit"
)

// Invoke the `kubeadm init` service

// Rewrite the server: in the .kube/config to point to localhost rather than an
// internal IP e.g. instead of
//   server: https://192.168.65.3:6443
// we should have
//   server: https://localhost:6443
func rewriteKubeConf(original string) string {
	return regexp.MustCompile(`server: https://[\d\.]+:6443`).ReplaceAllString(original, "server: https://localhost:6443")
}

func main() {
	// single-node-client -path <path to connect socket> [-init] [-expose]
	path := flag.String("path", os.Getenv("HOME")+"/Library/Containers/com.docker.docker/Data/connect", "path to connect socket")
	vpnkitPath := flag.String("vpnkit-control-path", os.Getenv("HOME")+"/Library/Containers/com.docker.docker/Data/s51", "path to vpnkit's control socket")
	expose := flag.Int("expose", 0, "TCP port to expose")
	port := flag.Int("port", 0xf3a3, "AF_VSOCK port to connect on")
	init := flag.Bool("init", false, "initialise the cluster")
	version := flag.String("version", "v1.7.2", "requested Kubernetes version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s: automate the setup of a single node kubernetes cluster.\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example usage:\n")
		fmt.Fprintf(os.Stderr, "%s -init -expose 6443\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "   -- initialise the cluster and expose on port 6443\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	// Create an http client that can talk over the Unix domain socket to the AF_VSOCK server
	httpc := http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				conn, err := d.DialContext(ctx, "unix", *path)
				if err != nil {
					return conn, err
				}
				// request the setup service
				remote := strings.NewReader(fmt.Sprintf("00000003.%08x\n", *port))
				_, err = io.Copy(conn, remote)
				if err != nil {
					log.Fatalf("Failed to write AF_VSOCK address %x to %s: %s", *port, *path, err)
				}
				return conn, err
			},
		},
	}

	if *init {
		// initialise the cluster
		// The NodeName will end up in the SSL certificate and must match the config file
		NodeName := "localhost"
		Version := *version
		req := common.InitRequest{NodeName: NodeName, Version: Version}
		reqBody := new(bytes.Buffer)
		json.NewEncoder(reqBody).Encode(req)
		response, err := httpc.Post("http://unix"+common.Init, "application/json", reqBody)
		if err != nil {
			log.Fatalf("Failed to invoke init: %s", err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			msg, err := ioutil.ReadAll(response.Body)
			if err != nil {
				log.Fatalf("Failed to read error message from init: %s", err)
			}
			log.Fatalf("Init failed with: %s", msg)
		}
		var res common.InitResponse
		err = json.NewDecoder(response.Body).Decode(&res)
		if err != nil {
			log.Fatalf("Failed to parse result of init: %s", err)
		}
		log.Printf("Init returned %v\n", res)
		conf := rewriteKubeConf(res.AdminConf)
		kubeDir := filepath.Join(os.Getenv("HOME"), ".kube")
		if err = os.MkdirAll(kubeDir, os.ModePerm); err != nil {
			log.Fatalf("Failed to create %s: %s", kubeDir, err)
		}
		configPath := filepath.Join(kubeDir, "config")
		if err = ioutil.WriteFile(configPath, []byte(conf), 0644); err != nil {
			log.Fatalf("Failed to write %s: %s", configPath, err)
		}
	}

	if *expose != 0 {
		response, err := httpc.Get("http://unix" + common.GetIP)
		if err != nil {
			log.Fatalf("Failed to invoke get_ip: %s", err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			msg, err := ioutil.ReadAll(response.Body)
			if err != nil {
				log.Fatalf("Failed to read error message from get_ip: %s", err)
			}
			log.Fatalf("get_ip failed with: %s", msg)
		}
		var res common.GetIPResponse
		err = json.NewDecoder(response.Body).Decode(&res)
		if err != nil {
			log.Fatalf("Failed to parse result of get_ip: %s", err)
		}
		log.Printf("get_ip returned %v\n", res)

		// expose the port
		c, err := vpnkit.NewConnection(context.Background(), *vpnkitPath)
		if err != nil {
			log.Fatal(err)
		}
		proto := "tcp"
		outIP := net.ParseIP("0.0.0.0")
		outPort := int16(*expose)
		inIP := net.ParseIP(res.IP)
		inPort := int16(*expose)
		p := vpnkit.NewPort(c, proto, outIP, outPort, inIP, inPort)
		if err = p.Expose(context.Background()); err != nil {
			log.Fatal(err)
		}
	}
}
