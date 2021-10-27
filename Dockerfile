ARG BASE_VARIANT=bullseye
ARG GO_VERSION=1.17.2
ARG XX_VERSION=1.0.0-rc.2

ARG LIBGIT2_IMG=ghcr.io/fluxcd/golang-with-libgit2
ARG LIBGIT2_TAG=libgit2-1.1.1-1

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx
FROM ${LIBGIT2_IMG}:${LIBGIT2_TAG} as libgit2

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${BASE_VARIANT} as gostable

FROM gostable AS go-linux

FROM go-${TARGETOS} AS build-base-bullseye

# Copy the build utiltiies
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

FROM libgit2-${BASE_VARIANT} as build-bullseye

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

FROM build-${BASE_VARIANT} as prepare-bullseye

# Move libgit2 lib to generic and predictable location
ARG TARGETPLATFORM
RUN mkdir -p /libgit2/lib/ \
    && cp -d /usr/lib/$(xx-info triple)/libgit2.so* /libgit2/lib/

FROM prepare-${BASE_VARIANT} as build

FROM debian:${BASE_VARIANT}-slim as controller

# Configure user
RUN groupadd controller && \
    useradd --gid controller --shell /bin/sh --create-home controller

# Copy libgit2
COPY --from=build /libgit2/lib/ /usr/local/lib/
RUN ldconfig

# Upgrade packages and install runtime dependencies
RUN echo "deb http://deb.debian.org/debian sid main" >> /etc/apt/sources.list \
    && echo "deb-src http://deb.debian.org/debian sid main" >> /etc/apt/sources.list \
    && apt update \
    && apt install --no-install-recommends -y zlib1g/sid libssl1.1/sid libssh2-1/sid \
    && apt install --no-install-recommends -y ca-certificates \
    && apt clean \
    && apt autoremove --purge -y \
    && rm -rf /var/lib/apt/lists/*

# Copy over binary from build
COPY --from=build /workspace/image-automation-controller /usr/local/bin/

USER controller
ENTRYPOINT [ "image-automation-controller" ]
