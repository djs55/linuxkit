#FROM linuxkit/alpine:9bcf61f605ef0ce36cc94d59b8eac307862de6e1 AS build
# XXX needs ebtables ethtool iproute2 libc6-compat socat
FROM alpine:3.6 AS build

ENV kubernetes_version v1.7.2
ENV weave_version      v2.0.1
ENV cni_version        v0.5.2

ENV kube_release_artefacts "https://dl.k8s.io/${kubernetes_version}/bin/linux/amd64"

RUN apk add -U --no-cache \
  curl \
  && true

RUN mkdir -p /out/etc/apk && cp -r /etc/apk/* /out/etc/apk/
RUN apk add --no-cache --initdb -p /out \
    alpine-baselayout \
    busybox \
    ca-certificates \
    curl \
    ebtables \
    ethtool \
    iproute2 \
    iptables \
    libc6-compat \
    musl \
    socat \
    util-linux \
    && true
# Remove apk residuals. We have a read-only rootfs, so apk is of no use.
RUN rm -rf /out/etc/apk /out/lib/apk /out/var/cache

RUN rmdir /out/var/run && ln -nfs /run /out/var/run

RUN curl -fSL -o /tmp/cni.tgz https://github.com/containernetworking/cni/releases/download/v0.5.2/cni-amd64-${cni_version}.tgz && \
    mkdir -p /out/opt/cni/bin /out/etc/cni/net.d && \
    tar -xzf /tmp/cni.tgz -C /out/opt/cni/bin
RUN curl -fSL -o /out/etc/weave.yaml https://cloud.weave.works/k8s/v1.6/net?v=${weave_version} 
RUN curl -fSL -o /out/usr/bin/kubelet https://dl.k8s.io/${kubernetes_version}/bin/linux/amd64/kubelet && chmod 0755 /out/usr/bin/kubelet
RUN curl -fSL -o /out/usr/bin/kubeadm https://dl.k8s.io/${kubernetes_version}/bin/linux/amd64/kubeadm && chmod 0755 /out/usr/bin/kubeadm
RUN curl -fSL -o /out/usr/bin/kubectl https://dl.k8s.io/${kubernetes_version}/bin/linux/amd64/kubectl && chmod 0755 /out/usr/bin/kubectl

ADD kubelet.sh /out/usr/bin/kubelet.sh
ADD kubeadm-init.sh /kubeadm-init.sh
RUN sed -e "s/@KUBERNETES_VERSION@/${kubernetes_version}/g" </kubeadm-init.sh >/out/usr/bin/kubeadm-init.sh && chmod +x /out/usr/bin/kubeadm-init.sh

FROM linuxkit/alpine:87a0cd10449d72f374f950004467737dbf440630 AS setup
RUN apk add --no-cache go musl-dev git build-base
ENV GOPATH=/go PATH=$PATH:/go/bin
ENV VIRTSOCK_COMMIT=a381dcc5bcddf1d7f449495c373dbf70f8e501c0
RUN mkdir -p $GOPATH/src/github.com/linuxkit && \
  cd $GOPATH/src/github.com/linuxkit && \
  git clone https://github.com/linuxkit/virtsock.git && \
  cd virtsock && \
  git checkout $VIRTSOCK_COMMIT
RUN mkdir -p /go/src/kube-setup-server
COPY cmd/server/*  /go/src/kube-setup-server/
RUN mkdir -p /go/src/github.com/linuxkit/linuxkit/projects/kubernetes/kubernetes/pkg/common
COPY pkg/common/* /go/src/github.com/linuxkit/linuxkit/projects/kubernetes/kubernetes/pkg/common
RUN go-compile.sh /go/src/kube-setup-server

FROM scratch
WORKDIR /
ENTRYPOINT ["/usr/bin/kubelet.sh"]
COPY --from=build /out /
COPY --from=setup /go/bin/kube-setup-server /usr/bin/kube-setup-server
ENV KUBECONFIG "/etc/kubernetes/admin.conf"
LABEL org.mobyproject.config='{"binds": ["/dev:/dev", "/etc/resolv.conf:/etc/resolv.conf", "/run:/run", "/var:/var:rshared,rbind", "/var/lib/kubeadm:/etc/kubernetes", "/etc/cni:/rootfs/etc/cni:rshared,rbind", "/opt/cni:/rootfs/opt/cni:rshared,rbind"], "mounts": [{"type": "cgroup", "options": ["rw","nosuid","noexec","nodev","relatime"]}], "capabilities": ["all"], "rootfsPropagation": "shared", "pid": "host"}'
