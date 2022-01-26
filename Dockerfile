ARG BASE_VARIANT=alpine
ARG GO_VERSION=1.17
ARG XX_VERSION=1.1.0

ARG LIBGIT2_IMG=ghcr.io/fluxcd/golang-with-libgit2
ARG LIBGIT2_TAG=libgit2-1.1.1-4

FROM --platform=linux/amd64 ${LIBGIT2_IMG}:${LIBGIT2_TAG} as build-amd64
FROM --platform=linux/arm64 ${LIBGIT2_IMG}:${LIBGIT2_TAG} as build-arm64
FROM --platform=linux/arm/v7 ${LIBGIT2_IMG}:${LIBGIT2_TAG} as build-armv7

FROM --platform=$BUILDPLATFORM build-$TARGETARCH$TARGETVARIANT AS build

# Configure workspace
WORKDIR /workspace

# This has its own go.mod, which needs to be present so go mod
# download works.
COPY api/ api/

# Copy modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

RUN apk add clang lld pkgconfig ca-certificates

ENV CGO_ENABLED=1
ARG TARGETPLATFORM

RUN xx-apk add --no-cache \
        musl-dev gcc lld binutils-gold

# Performance related changes:
# - Use read-only bind instead of copying go source files.
# - Cache go packages.
RUN --mount=target=. \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    export LIBRARY_PATH="/usr/local/$(xx-info triple)/lib:/usr/local/$(xx-info triple)/lib64:${LIBRARY_PATH}" && \
    export PKG_CONFIG_PATH="/usr/local/$(xx-info triple)/lib/pkgconfig:/usr/local/$(xx-info triple)/lib64/pkgconfig" && \
	export FLAGS="$(pkg-config --static --libs --cflags libssh2 openssl libgit2)" && \
    CGO_LDFLAGS="${FLAGS} -static" \
	xx-go build \
        -ldflags "-s -w" \
        -tags 'netgo,osusergo,static_build' \
        -o /image-automation-controller -trimpath \
    main.go

# Ensure that the binary was cross-compiled correctly to the target platform.
RUN xx-verify --static /image-automation-controller


FROM alpine:3.15

ARG TARGETPLATFORM
RUN apk --no-cache add ca-certificates \
  && update-ca-certificates

# Create minimal nsswitch.conf file to prioritize the usage of /etc/hosts over DNS queries.
# https://github.com/gliderlabs/docker-alpine/issues/367#issuecomment-354316460
RUN [ ! -e /etc/nsswitch.conf ] && echo 'hosts: files dns' > /etc/nsswitch.conf

# Copy over binary from build
COPY --from=build /image-automation-controller /usr/local/bin/
COPY ATTRIBUTIONS.md /

USER 65534:65534
ENTRYPOINT [ "image-automation-controller" ]
