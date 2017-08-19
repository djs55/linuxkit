#!/bin/sh

mount --bind /opt/cni /rootfs/opt/cni
mount --bind /etc/cni /rootfs/etc/cni

/usr/bin/kube-setup-server

