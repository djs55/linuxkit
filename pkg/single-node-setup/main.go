package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/linuxkit/virtsock/pkg/vsock"
)

// Expose a service to run `kubeadm` in a single node (desktop) environment

// Init is the /path for the `kubeadm init` RPC
const Init = "/init"

// InitRequest is the arguments for `kubeadm init`
type InitRequest struct {
	NodeName string `json:"node_name"` // used in the certificate. Must resolve on the host to a local interface
}

// InitResponse returns the response from `kubeadm init`
type InitResponse struct {
	AdminConf string `json:"admin_conf"` // the admin.conf containing the private keys
}

// Expose is the /path for the request to expose the HTTPS port on the host
const Expose = "/expose"

// ExposeRequest is the arguments for exposing the port
type ExposeRequest struct {
	ExternalPort int `json:"external_port"` // the port on the host
}

// HTTPListener responds to HTTP on the AF_VSOCK listening socket
type HTTPListener struct {
	Listener net.Listener // where to listen
}

// Serve responds to HTTP requests forever
func (h HTTPListener) Serve() error {
	http.HandleFunc("/", h.pingHandler())
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

func main() {
	// single-node-setup -vsock <port>
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
