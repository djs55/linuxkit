package main

import (
	"encoding/json"
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

func doInit(req InitRequest) (InitResponse, error) {
	log.Printf("Received init request %v", req)
	AdminConf := ""
	res := InitResponse{AdminConf}
	return res, nil
}

// Expose is the /path for the request to expose the HTTPS port on the host
const Expose = "/expose"

// ExposeRequest is the arguments for exposing the port
type ExposeRequest struct {
	ExternalPort int `json:"external_port"` // the port on the host
}

func doExpose(req ExposeRequest) error {
	log.Printf("Received expose request %v", req)
	return nil
}

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
		err = doExpose(req)
		if err != nil {
			http.Error(w, err.Error(), 400)
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
