// An HTTP-over-vsock service which initialises a single node Kubernetes cluster
// returning the keys to the client and optionally exposing the main API port.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/linuxkit/virtsock/pkg/vsock"
)

// Init is the /path for the `kubeadm init` RPC
const Init = "/init"

// InitRequest is the arguments for `kubeadm init`
type InitRequest struct {
	NodeName string `json:"node_name"` // used in the certificate. Must resolve on the host to a local interface
	Version  string `json:"version"`   // requested Kubernetes version
}

// InitResponse returns the response from `kubeadm init`
type InitResponse struct {
	AdminConf string `json:"admin_conf"` // the admin.conf containing the private keys
}

func doInit(req InitRequest) (*InitResponse, error) {
	log.Printf("Received init request %v", req)

	args := []string{"init", "--skip-preflight-checks", "--node-name", req.NodeName, "--kubernetes-version", req.Version}
	if err := exec.Command("/usr/bin/kubeadm", args...).Run(); err != nil {
		log.Printf("Failed to run kubeadm %s: %s", strings.Join(args, " "), err)
		return nil, err
	}
	args = []string{"create", "-n", "kube-system", "-f", "/etc/weave.yaml"}
	if err := exec.Command("/usr/bin/kubectl", args...).Run(); err != nil {
		log.Printf("Failed to run kubectl %s: %s", strings.Join(args, " "), err)
		return nil, err
	}
	// read /etc/kubernetes/admin.conf
	b, err := ioutil.ReadFile("/etc/kubernetes/admin.conf")
	if err != nil {
		log.Printf("Failed to read /etc/kubernetes/admin.conf: %s", err)
		return nil, err
	}
	return &InitResponse{AdminConf: string(b)}, nil
}

// Expose is the /path for the request to expose the HTTPS port on the host
const Expose = "/expose"

// ExposeRequest is the arguments for exposing the port
type ExposeRequest struct {
	ExternalPort int `json:"external_port"` // the port on the host
}

// ExposeResponse returns the response from exposing the port
type ExposeResponse struct {
}

func doExpose(req ExposeRequest) (*ExposeResponse, error) {
	log.Printf("Received expose request %v", req)

	// Discover the IP on the same subnet as the vpnkit gateway, so we tell vpnkit
	// the correct IP to use to call us back on.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Printf("Failed to discover an external IP address: is there a default route? %s", err)
		return nil, err
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().String()
	idx := strings.LastIndex(localAddr, ":")
	containerIP := localAddr[0:idx]
	log.Printf("Discovered local external IP address is: %s", containerIP)

	args := []string{"-container-ip", containerIP, "-container-port", "6443", "-host-ip", "0.0.0.0", "-host-port", fmt.Sprintf("%d", req.ExternalPort), "-no-local-ip"}
	cmd := exec.Command("/usr/bin/vpnkit-expose-port", args...)

	// We need a pipe to receive success / failure
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	cmd.ExtraFiles = []*os.File{w}
	log.Printf("Starting %s", strings.Join(cmd.Args, " "))
	if err = cmd.Start(); err != nil {
		w.Close()
		log.Printf("Failed to start vpnkit-expose-port: %s", err)
		return nil, err
	}
	w.Close()
	b := bufio.NewReader(r)
	line, err := b.ReadString('\n')
	if err != nil {
		log.Printf("Failed to read response code from vpnkit-expose-port: %s", err)
		return nil, err
	}
	if line == "1\n" {
		msg, err := ioutil.ReadAll(b)
		if err != nil {
			log.Printf("Failed to read the error message from vpnkit-expose-port: %s", err)
		}
		return nil, errors.New(string(msg))
	}
	// Leave the process running
	return &ExposeResponse{}, nil
}

// HTTP server follows:

// HTTPListener responds to HTTP on the AF_VSOCK listening socket
type HTTPListener struct {
	Listener net.Listener // where to listen
}

// Serve responds to HTTP requests forever
func (h HTTPListener) Serve() error {
	http.HandleFunc("/", h.pingHandler())
	http.HandleFunc("/init", h.initHandler())
	http.HandleFunc("/expose", h.exposeHandler())
	server := &http.Server{}
	return server.Serve(h.Listener)
}

const pingOK = "LGTM"

// Return a handler for pinging this server
func (h HTTPListener) pingHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(pingOK)); err != nil {
			log.Println("Error writing HTTP success response:", err)
			return
		}
	}
}

// Return a handler for invoking `kubeadm init`
func (h HTTPListener) initHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req InitRequest
		if r.Body == nil {
			http.Error(w, "Please send a request body", 400)
			return
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, err := doInit(req)
		if err != nil {
			log.Printf("Returning error %s", err)
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(res)
	}
}

// Return a handler for exposing the port
func (h HTTPListener) exposeHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ExposeRequest
		if r.Body == nil {
			http.Error(w, "Please send a request body", 400)
			return
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, err := doExpose(req)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(res)
	}
}

func main() {
	// single-node-setup -cid <cid> -vsock <port>
	cid := flag.Int("cid", 0, "AF_VSOCK CID to listen on")
	port := flag.Int("port", 0xf3a3, "AF_VSOCK port to listen on")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s: automate the setup of a single node kubernetes cluster.\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example usage:\n")
		fmt.Fprintf(os.Stderr, "%s [-cid <cid>] [-port 0xf3a3]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "   -- listen for incoming HTTP RPCs on the AF_VSOCK cid and port\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	if *cid == 0 {
		// by default allow connections from anywhere on the local machine
		*cid = vsock.CIDAny
	}
	l, err := vsock.Listen(uint32(*cid), uint32(*port))
	if err != nil {
		log.Fatalf("Failed to bind to vsock port %x:%x: %s", *cid, *port, err)
	}
	log.Printf("Listening on port %x:%x", *cid, *port)
	h := HTTPListener{l}

	err = h.Serve()
	if err != nil {
		log.Fatalf("Failed to Serve HTTP: %s", err)
	}
}
