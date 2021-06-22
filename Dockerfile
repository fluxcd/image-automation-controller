FROM golang:1.16-buster as builder

# Up-to-date libgit2 dependencies are only available in sid (unstable).
# The libgit2 dependencies must be listed here to be able to build on ARM64.
RUN echo "deb http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list \
    && echo "deb-src http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list
RUN set -eux; \
    apt-get update \
    && apt-get install -y libgit2-dev/unstable zlib1g-dev/unstable libssh2-1-dev/unstable libpcre3-dev/unstable \
    && apt-get clean \
    && apt-get autoremove --purge -y \
    && rm -rf /var/lib/apt/lists/*

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
RUN CGO_ENABLED=1 go build -o image-automation-controller main.go

FROM debian:buster-slim as controller

LABEL org.opencontainers.image.source="https://github.com/fluxcd/image-automation-controller"

# Up-to-date libgit2 dependencies are only available in
# unstable, as libssh2 in testing/bullseye has been linked
# against gcrypt which causes issues with PKCS* formats.
RUN echo "deb http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list \
    && echo "deb-src http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list
RUN set -eux; \
    apt-get update \
    && apt-get install -y ca-certificates libgit2-1.1 \
    && apt-get clean \
    && apt-get autoremove --purge -y \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /workspace/image-automation-controller /usr/local/bin/

RUN groupadd controller && \
    useradd --gid controller --shell /bin/sh --create-home controller

USER controller

ENTRYPOINT [ "image-automation-controller" ]
