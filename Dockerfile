ARG BASE_VARIANT=bullseye
ARG GO_VERSION=1.17
ARG XX_VERSION=1.1.0

ARG LIBGIT2_IMG=ghcr.io/fluxcd/golang-with-libgit2
ARG LIBGIT2_TAG=libgit2-1.1.1-3

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx
FROM ${LIBGIT2_IMG}:${LIBGIT2_TAG} as libgit2

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${BASE_VARIANT} as gostable

FROM gostable AS go-linux

FROM go-${TARGETOS} AS build-base-bullseye

# Copy the build utilities
COPY --from=xx / /
COPY --from=libgit2 /Makefile /libgit2/

# Install the libgit2 build dependencies
RUN make -C /libgit2 cmake

ARG TARGETPLATFORM
RUN make -C /libgit2 dependencies

FROM build-base-${BASE_VARIANT} as libgit2-bullseye

# Compile and install libgit2
ARG TARGETPLATFORM
RUN FLAGS=$(xx-clang --print-cmake-defines) make -C /libgit2 libgit2

FROM libgit2-${BASE_VARIANT} as build

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

# Copy the go source
COPY main.go main.go
COPY pkg/ pkg/
COPY controllers/ controllers/

# Build the binary
ENV CGO_ENABLED=1
ARG TARGETPLATFORM
RUN xx-go build -o image-automation-controller -trimpath \
    main.go

FROM build as prepare-bullseye

# Move libgit2 lib to generic and predictable location
ARG TARGETPLATFORM
RUN mkdir -p /libgit2/lib/ \
    && cp -d /usr/lib/$(xx-info triple)/libgit2.so* /libgit2/lib/

FROM prepare-${BASE_VARIANT} as prepare

# The target image must aligned with apt sources used for libgit2.
FROM debian:bookworm-slim as controller

# Copy libgit2
COPY --from=prepare /libgit2/lib/ /usr/local/lib/
RUN ldconfig

# Upgrade packages and install runtime dependencies
RUN apt update \
    && apt install -y zlib1g libssl1.1 libssh2-1 ca-certificates \
    && apt clean \
    && apt autoremove --purge -y \
    && rm -rf /var/lib/apt/lists/*

# Copy over binary from build
COPY --from=prepare /workspace/image-automation-controller /usr/local/bin/

USER 65534:65534

ENTRYPOINT [ "image-automation-controller" ]
