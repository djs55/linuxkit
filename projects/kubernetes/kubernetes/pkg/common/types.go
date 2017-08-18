// API for the kube-setup service
package common

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

// GetIP is the /path for the request to query the external IP of the VM suitable
// for port forwarding via vpnkit
const GetIP = "/get_ip"

// GetIPResponse returns the response from exposing the port
type GetIPResponse struct {
	IP string `json:"ip"` // IP of the master
}
