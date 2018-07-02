FROM linuxkit/alpine:1b05307ae8152e3d38f79e297b0632697a30c65c AS build

RUN apk add --no-cache go musl-dev
ENV GOPATH=/go PATH=$PATH:/go/bin

COPY logwrite.go /go/src/logwrite/
RUN go-compile.sh /go/src/logwrite

FROM scratch
ENTRYPOINT []
CMD []
WORKDIR /
COPY --from=build /go/bin/logwrite usr/bin/logwrite
CMD ["/usr/bin/logwrite"]
