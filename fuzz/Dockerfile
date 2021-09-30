FROM golang:1.16-buster as builder

# Up-to-date libgit2 dependencies are only available in sid (unstable).
# The libgit2 dependencies must be listed here to be able to build on ARM64.
RUN echo "deb http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list \
    && echo "deb-src http://deb.debian.org/debian unstable main" >> /etc/apt/sources.list
RUN set -eux; \
    apt-get update \
    && apt-get install -y vim clang cmake libgit2-dev/unstable \
        zlib1g-dev/unstable libssh2-1-dev/unstable libpcre3-dev/unstable \
    && apt-get clean \
    && apt-get autoremove --purge -y \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir /static_a_files
ARG LIBGIT2_VER=1.1.0
RUN curl -L https://github.com/libgit2/libgit2/releases/download/v$LIBGIT2_VER/libgit2-$LIBGIT2_VER.tar.gz -o /tmp/libgit2.tar.gz \
    && cd /tmp \
    && tar -xvf /tmp/libgit2.tar.gz \
    && cd libgit2-1.1.0 \
    && mkdir build && cd build \
    && cmake .. -DBUILD_SHARED_LIBS=OFF \
    && make \
    && mv libgit2.a /static_a_files/

# Build openssl
ARG OPENSSL_VERSION=1.1.1g
ARG OPENSSL_HASH=ddb04774f1e32f0c49751e21b67216ac87852ceb056b75209af2443400636d46
RUN set -ex \
    && curl -s -O https://www.openssl.org/source/openssl-${OPENSSL_VERSION}.tar.gz \
    && echo "${OPENSSL_HASH}  openssl-${OPENSSL_VERSION}.tar.gz" | sha256sum -c \
    && tar -xzf openssl-${OPENSSL_VERSION}.tar.gz \
    && cd openssl-${OPENSSL_VERSION} \
    && ./Configure linux-x86_64 no-shared --static \
    && make \
    && mv libcrypto.a /static_a_files/ \
    && mv libssl.a /static_a_files/

# Build libssh2
RUN git clone https://github.com/libssh2/libssh2 \
    && cd libssh2 \
    && mkdir build \
    && cd build \
    && cmake .. -DBUILD_SHARED_LIBS=OFF \
    && make \
    && mv ./src/libssh2.a /static_a_files/

RUN git clone --depth=1 --branch=fuzz1 https://github.com/AdamKorcz/image-automation-controller /image-automation-controller
WORKDIR /image-automation-controller

RUN cd / \
    &&  go get -u github.com/dvyukov/go-fuzz/go-fuzz@latest github.com/dvyukov/go-fuzz/go-fuzz-build@latest \
    && go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
RUN go get github.com/AdaLogics/go-fuzz-headers

RUN cd / \
    && git clone https://github.com/fluxcd/source-controller \
    && cd source-controller/config/crd/bases \
    && mkdir /image-automation-controller/controllers/testdata/crds \
    && cp ./* /image-automation-controller/controllers/testdata/crds/


RUN cd / \
    && git clone https://github.com/fluxcd/image-reflector-controller \
    && cd image-reflector-controller/config/crd/bases \
    && cp ./* /image-automation-controller/controllers/testdata/crds/

RUN mv ./fuzz/fuzz.go ./controllers/
RUN cd /image-automation-controller/controllers/ \
    && go mod tidy
RUN go mod download github.com/dvyukov/go-fuzz

RUN mkdir /fuzzers
RUN cd controllers \
    && go-fuzz-build -libfuzzer -func=FuzzReconciler \
    && clang -o /fuzzers/FuzzReconciler reflect-fuzz.a \
    /static_a_files/libgit2.a \
    /static_a_files/libssh2.a \
    /static_a_files/libssl.a \
    /static_a_files/libcrypto.a \
    -lpcre -lz \
    -fsanitize=fuzzer
RUN cd controllers \
    && go-fuzz-build -libfuzzer -func=FuzzUpdateWithSetters \
    && clang -o /fuzzers/FuzzUpdateWithSetters reflect-fuzz.a \
    /static_a_files/libgit2.a \
    /static_a_files/libssh2.a \
    /static_a_files/libssl.a \
    /static_a_files/libcrypto.a \
    -lpcre -lz \
    -fsanitize=fuzzer

# Run fuzzer from /image-automation-controller/controllers
#RUN cd controllers && /fuzzers/FuzzUpdateWithSetters
#RUN cd controllers && /fuzzers/FuzzReconciler
