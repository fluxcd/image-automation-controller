FROM gcr.io/oss-fuzz-base/base-builder-go

ENV SRC=$GOPATH/src/github.com/fluxcd/image-automation-controller
ENV ROOT_ORG=$SRC
ENV FLUX_CI=true

COPY ./ $GOPATH/src/github.com/fluxcd/image-automation-controller/
RUN wget https://raw.githubusercontent.com/google/oss-fuzz/master/projects/fluxcd/build.sh -O $SRC/build.sh

WORKDIR $SRC
