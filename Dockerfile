ARG BASE_VARIANT=alpine
ARG GO_VERSION=1.24
ARG XX_VERSION=1.6.1

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${BASE_VARIANT} AS gostable

FROM gostable AS go-linux

# Build-base consists of build platform dependencies and xx.
# These will be used at current arch to yield execute the cross compilations.
FROM go-${TARGETOS} AS build-base

RUN apk add clang lld

COPY --from=xx / /

# build can still be cached at build platform architecture.
FROM build-base AS build

ARG TARGETPLATFORM

# Some dependencies have to installed for the target platform:
# https://github.com/tonistiigi/xx#go--cgo
RUN xx-apk add musl-dev gcc

# Configure workspace
WORKDIR /workspace

# Copy api submodule
COPY api/ api/

# Copy modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache modules
RUN go mod download

# Copy source code
COPY main.go main.go
COPY pkg/ pkg/
COPY internal/ internal/

ARG TARGETPLATFORM
ARG TARGETARCH

# Reasons why CGO is in use:
# - The SHA1 implementation (sha1cd) used by go-git depends on CGO for
#   performance reasons. See: https://github.com/pjbgf/sha1cd/issues/15
ENV CGO_ENABLED=1

RUN export CGO_LDFLAGS="-static -fuse-ld=lld" && \
  xx-go build  \
  -ldflags "-s -w" \
  -tags 'netgo,osusergo,static_build' \
  -o /image-automation-controller -trimpath main.go;

# Ensure that the binary was cross-compiled correctly to the target platform.
RUN xx-verify --static /image-automation-controller

FROM alpine:3.21

ARG TARGETPLATFORM
RUN apk --no-cache add ca-certificates \
  && update-ca-certificates

# Copy over binary from build
COPY --from=build /image-automation-controller /usr/local/bin/

USER 65534:65534
ENTRYPOINT [ "image-automation-controller" ]
