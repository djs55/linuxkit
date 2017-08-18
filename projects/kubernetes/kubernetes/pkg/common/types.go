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
	AdminConf  string `json:"admin_conf"`  // the admin.conf containing the private keys
	InternalIP string `json:"internal_ip"` // IP of the master
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
