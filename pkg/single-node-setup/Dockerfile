# We need the `fstrim` binary:
FROM linuxkit/alpine:87a0cd10449d72f374f950004467737dbf440630 AS mirror

RUN apk add --no-cache go musl-dev
ENV GOPATH=/go PATH=$PATH:/go/bin

COPY .  /go/src/single-node-setup
RUN go-compile.sh /go/src/single-node-setup

FROM scratch
ENTRYPOINT []
CMD []
WORKDIR /
COPY --from=mirror /go/bin/single-node-setup /usr/bin/single-node-setup
CMD ["/usr/bin/single-node-setup"]
LABEL org.mobyproject.config='{"binds": ["/var/run:/var/run", "/var/lib/docker:/var/lib/docker"], "capabilities": ["CAP_SYS_ADMIN"]}'
