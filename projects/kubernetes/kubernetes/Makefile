ORG?=linuxkitprojects
IMAGE=kubernetes
NETWORK=1
NOTRUST=1

include ../../../pkg/package.mk

.PHONY: depends
depends:
	trash --keep

cmd/client:
	cd cmd; go build
