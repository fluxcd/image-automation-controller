FROM golang:1.16-buster as builder

# Up-to-date libgit2 dependencies are only available in sid (unstable).
RUN echo "deb http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list \
    && echo "deb-src http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list
RUN set -eux; \
    apt-get update \
    && apt-get install -y libgit2-dev/unstable zlib1g-dev/unstable libssh2-1-dev/unstable libpcre3-dev/unstable \
    && apt-get clean \
    && apt-get autoremove --purge -y \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd -g 116 test && \
    useradd -u 1001 --gid test --shell /bin/sh --create-home test

# Run as test user
USER test

ENTRYPOINT ["/bin/sh", "-c"]
