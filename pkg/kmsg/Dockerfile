FROM linuxkit/alpine:1b05307ae8152e3d38f79e297b0632697a30c65c AS mirror

RUN apk add --no-cache go musl-dev linux-headers
ENV GOPATH=/go PATH=$PATH:/go/bin

COPY .  /go/src/kmsg/
RUN go-compile.sh /go/src/kmsg

FROM scratch
ENTRYPOINT []
CMD []
WORKDIR /
COPY --from=mirror /go/bin/kmsg /usr/bin/kmsg
CMD ["/usr/bin/kmsg"]
