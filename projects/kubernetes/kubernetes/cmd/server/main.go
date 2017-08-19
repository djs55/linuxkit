// An HTTP-over-vsock service which initialises a single node Kubernetes cluster
// returning the keys to the client and optionally exposing the main API port.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/linuxkit/linuxkit/projects/kubernetes/kubernetes/pkg/common"
	"github.com/linuxkit/virtsock/pkg/vsock"
)

func doInit(req common.InitRequest) (*common.InitResponse, error) {
	log.Printf("Received init request %v", req)

	args := []string{"init", "--skip-preflight-checks", "--node-name", req.NodeName, "--kubernetes-version", req.Version}
	kubeadm := exec.Command("/usr/bin/kubeadm", args...)
	kubeadm.Stdout = os.Stderr
	kubeadm.Stderr = os.Stderr
	if err := kubeadm.Start(); err != nil {
		log.Printf("Failed to run kubeadm %s: %s", strings.Join(args, " "), err)
		return nil, err
	}
	log.Printf("Launched /usr/bin/kubeadm")

	go func() {
		// Start the kubelet in the background. This will fail until the directory
		// structure has been created by `kubeadm init` above. The `kubeadm init`
		// will block until the `kubelet` has done some work.
		args = []string{"--kubeconfig=/var/lib/kubeadm/kubelet.conf",
			"--require-kubeconfig=true",
			"--pod-manifest-path=/var/lib/kubeadm/manifests",
			"--allow-privileged=true",
			"--cluster-dns=10.96.0.10",
			"--hostname-override=" + req.NodeName,
			"--cluster-domain=cluster.local",
			"--cgroups-per-qos=false",
			"--enforce-node-allocatable=",
			"--network-plugin=cni",
			"--cni-conf-dir=/etc/cni/net.d",
			"--cni-bin-dir=/opt/cni/bin"}
		for {
			kubelet := exec.Command("/usr/bin/kubelet", args...)
			kubelet.Stdout = os.Stderr
			kubelet.Stderr = os.Stderr
			if err := kubelet.Run(); err != nil {
				log.Printf("Failed to run kubelet %s: %s", strings.Join(args, " "), err)
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()

	// Wait for kubeadm to complete
	if err := kubeadm.Wait(); err != nil {
		return nil, err
	}
	log.Printf("/usr/bin/kubeadm complete")

	args = []string{"create", "-n", "kube-system", "-f", "/etc/weave.yaml"}
	kubectl := exec.Command("/usr/bin/kubectl", args...)
	kubectl.Stdout = os.Stderr
	kubectl.Stderr = os.Stderr
	if err := kubectl.Run(); err != nil {
		log.Printf("Failed to run kubectl %s: %s", strings.Join(args, " "), err)
		return nil, err
	}

	for {
		// read /etc/kubernetes/admin.conf
		b, err := ioutil.ReadFile("/etc/kubernetes/admin.conf")
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			log.Printf("Failed to read /etc/kubernetes/admin.conf: %s", err)
			return nil, err
		}
		AdminConf := string(b)

		return &common.InitResponse{AdminConf}, nil
	}
}

func doGetIP() (*common.GetIPResponse, error) {
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
	IP := localAddr[0:idx]
	log.Printf("Discovered local external IP address is: %s", IP)
	return &common.GetIPResponse{IP}, nil
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
	http.HandleFunc("/get_ip", h.getIPHandler())
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
		var req common.InitRequest
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

// Return a handler for querying the IP
func (h HTTPListener) getIPHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := doGetIP()
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
