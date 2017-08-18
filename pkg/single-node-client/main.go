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
	"strings"
)

// Invoke the `kubeadm init` service

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

// ExposeResponse returns the response from exposing the port
type ExposeResponse struct {
}

func main() {
	// single-node-client -path <path to connect socket> [-init] [-expose]
	path := flag.String("path", os.Getenv("HOME")+"/Library/Containers/com.docker.docker/Data/connect", "path to connect socket")
	expose := flag.Int("expose", 0, "TCP port to expose")
	port := flag.Int("port", 0xf3a3, "AF_VSOCK port to connect on")
	init := flag.Bool("init", false, "initialise the cluster")
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
		NodeName, err := os.Hostname()
		if err != nil {
			log.Fatalf("Failed to determine the local hostname: %s", err)
		}
		req := InitRequest{NodeName}
		reqBody := new(bytes.Buffer)
		json.NewEncoder(reqBody).Encode(req)
		response, err := httpc.Post("http://unix"+Init, "application/json", reqBody)
		if err != nil {
			log.Fatalf("Failed to invoke init: %s", err)
		}
		var res InitResponse
		err = json.NewDecoder(response.Body).Decode(&res)
		if err != nil {
			log.Fatalf("Failed to parse result of init: %s", err)
		}
		log.Printf("Init returned %v\n", res)
		kubeDir := filepath.Join(os.Getenv("HOME"), ".kube")
		if err = os.MkdirAll(kubeDir, os.ModePerm); err != nil {
			log.Fatalf("Failed to create %s: %s", kubeDir, err)
		}
		configPath := filepath.Join(kubeDir, "config")
		if err = ioutil.WriteFile(configPath, []byte(res.AdminConf), 0644); err != nil {
			log.Fatalf("Failed to write %s: %s", configPath, err)
		}
		response.Body.Close()
	}

	if *expose != 0 {
		// expose the port
		req := ExposeRequest{ExternalPort: *expose}
		reqBody := new(bytes.Buffer)
		json.NewEncoder(reqBody).Encode(req)
		response, err := httpc.Post("http://unix"+Expose, "application/json", reqBody)
		if err != nil {
			log.Fatalf("Failed to invoke expose: %s", err)
		}
		var res ExposeResponse
		err = json.NewDecoder(response.Body).Decode(&res)
		if err != nil {
			log.Fatalf("Failed to parse result of expose: %s", err)
		}
		log.Printf("Expose returned %v\n", res)
		response.Body.Close()
	}
}
