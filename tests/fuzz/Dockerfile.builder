FROM gcr.io/oss-fuzz-base/base-builder-go

RUN apt-get update && apt-get install -y cmake pkg-config

COPY ./ $GOPATH/src/github.com/fluxcd/image-automation-controller/
COPY ./tests/fuzz/oss_fuzz_build.sh $SRC/build.sh

WORKDIR $SRC
