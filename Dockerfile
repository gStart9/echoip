# Build
FROM golang:1.24 AS build
WORKDIR /go/src/github.com/mpolden/echoip
COPY . .

# Must build without cgo because libc is unavailable in runtime image
ENV CGO_ENABLED=0
RUN make

# Run
FROM scratch
EXPOSE 8080

COPY --from=build /go/bin/echoip /go/bin/echoip-geoupdate /opt/echoip/
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY html /opt/echoip/html

WORKDIR /opt/echoip
ENTRYPOINT ["/opt/echoip/echoip"]
