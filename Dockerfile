FROM golang:1.15-alpine as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# This has its own go.mod, which needs to be present so go mod
# download works.
COPY api/ api/

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY pkg/ pkg/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 go build -a -o image-automation-controller main.go

FROM alpine:3.12

LABEL org.opencontainers.image.source="https://github.com/fluxcd/image-automation-controller"

RUN apk add --no-cache ca-certificates tini

COPY --from=builder /workspace/image-automation-controller /usr/local/bin/

# Create minimal nsswitch.conf file to prioritize the usage of /etc/hosts over DNS queries.
# https://github.com/gliderlabs/docker-alpine/issues/367#issuecomment-354316460
RUN [ ! -e /etc/nsswitch.conf ] && echo 'hosts: files dns' > /etc/nsswitch.conf

RUN addgroup -S controller && adduser -S -g controller controller

USER controller

ENTRYPOINT [ "/sbin/tini", "--", "image-automation-controller" ]
