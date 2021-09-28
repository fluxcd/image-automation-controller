ARG BASE_IMG=ghcr.io/hiddeco/golang-with-libgit2
ARG BASE_TAG=dev
FROM ${BASE_IMG}:${BASE_TAG} AS build

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

FROM debian:bullseye-slim as controller

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
