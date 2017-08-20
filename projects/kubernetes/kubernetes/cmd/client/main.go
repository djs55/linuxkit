package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/linuxkit/linuxkit/projects/kubernetes/kubernetes/pkg/common"
)

// Invoke the `kubeadm init` service

func main() {
	// single-node-client -path <path to connect socket> [-init] [-expose]
	path := flag.String("path", os.Getenv("HOME")+"/Library/Containers/com.docker.docker/Data/connect", "path to connect socket")
	vpnkitPath := flag.String("vpnkit-control-path", os.Getenv("HOME")+"/Library/Containers/com.docker.docker/Data/s51", "path to vpnkit's control socket")
	expose := flag.Int("expose", 6443, "TCP port to expose")
	port := flag.Int("port", 0xf3a3, "AF_VSOCK port to connect on")
	version := flag.String("version", "v1.7.2", "requested Kubernetes version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s: automate the setup of a single node kubernetes cluster.\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example usage:\n")
		fmt.Fprintf(os.Stderr, "%s -expose 6443\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "   -- initialise the cluster and expose on port 6443\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	c := &common.Configuration{
		ExternalPort:    uint16(*expose),
		Version:         *version,
		HyperkitConnect: *path,
		VpnkitControl:   *vpnkitPath,
		SetupPort:       *port,
	}
	if err := c.Setup(); err != nil {
		log.Fatal(err)
	}
}
