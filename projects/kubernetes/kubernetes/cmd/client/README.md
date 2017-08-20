A simple client to demonstrate how to trigger the cluster setup.

To use, first boot a single VM in one terminal:

```
cd linuxkit/project/kubernetes
./boot.sh
```

Then open a second terminal and trigger the setup:

```
cd linuxkit/project/kubernetes/kubernetes/cmd/client
./client -path ../../../kube-master-state/connect
```

Once the `client` command exits, then run:

```
kubectl get nodes
```
