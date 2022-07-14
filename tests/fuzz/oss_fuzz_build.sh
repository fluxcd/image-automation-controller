#!/usr/bin/env bash

# Copyright 2022 The Flux authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euxo pipefail

LIBGIT2_TAG="${LIBGIT2_TAG:-v0.1.2}"
GOPATH="${GOPATH:-/root/go}"
GO_SRC="${GOPATH}/src"
PROJECT_PATH="github.com/fluxcd/image-automation-controller"

pushd "${GO_SRC}/${PROJECT_PATH}"

export TARGET_DIR="$(/bin/pwd)/build/libgit2/${LIBGIT2_TAG}"

# For most cases, libgit2 will already be present.
# The exception being at the oss-fuzz integration.
if [ ! -d "${TARGET_DIR}" ]; then
    curl -o output.tar.gz -LO "https://github.com/fluxcd/golang-with-libgit2/releases/download/${LIBGIT2_TAG}/linux-$(uname -m)-all-libs.tar.gz"

    DIR=libgit2-linux-all-libs
    NEW_DIR="$(/bin/pwd)/build/libgit2/${LIBGIT2_TAG}"
    INSTALLED_DIR="/home/runner/work/golang-with-libgit2/golang-with-libgit2/build/${DIR}"

    mkdir -p ./build/libgit2

    tar -xf output.tar.gz
    rm output.tar.gz
    mv "${DIR}" "${LIBGIT2_TAG}"
    mv "${LIBGIT2_TAG}/" "./build/libgit2"

    # Update the prefix paths included in the .pc files.
    # This will make it easier to update to the location in which they will be used.
    find "${NEW_DIR}" -type f -name "*.pc" | xargs -I {} sed -i "s;${INSTALLED_DIR};${NEW_DIR};g" {}
fi

apt-get update && apt-get install -y pkg-config

export CGO_ENABLED=1
export LIBRARY_PATH="${TARGET_DIR}/lib:${TARGET_DIR}/lib64"
export PKG_CONFIG_PATH="${TARGET_DIR}/lib/pkgconfig:${TARGET_DIR}/lib64/pkgconfig"
export CGO_CFLAGS="-I${TARGET_DIR}/include -I${TARGET_DIR}/include/openssl"
export CGO_LDFLAGS="$(pkg-config --libs --static --cflags libssh2 openssl libgit2)"

pushd "tests/fuzz"

# Setup files to be embedded into controllers_fuzzer.go's testFiles variable.
mkdir -p testdata/crds
cp ../../config/crd/bases/*.yaml testdata/crds/

# Use main go.mod in order to conserve the same version across all dependencies.
cp ../../go.mod .
cp ../../go.sum .

sed -i 's;module .*;module github.com/fluxcd/image-automation-controller/tests/fuzz;g' go.mod
sed -i 's;api => ./api;api => ../../api;g' go.mod
echo "replace github.com/fluxcd/image-automation-controller => ../../" >> go.mod

# Version of the source-controller from which to get the GitRepository CRD.
# Pulls source-controller/api's version set in go.mod.
SOURCE_VER=$(go list -m github.com/fluxcd/source-controller/api | awk '{print $2}')

# Version of the image-reflector-controller from which to get the ImagePolicy CRD.
# Pulls image-reflector-controller/api's version set in go.mod.
REFLECTOR_VER=$(go list -m github.com/fluxcd/image-reflector-controller/api | awk '{print $2}')

go mod download
go mod tidy -go=1.18
go get -d github.com/fluxcd/image-automation-controller
go get -d github.com/AdaLogics/go-fuzz-headers

if [ -d "../../controllers/testdata/crds" ]; then
    cp ../../controllers/testdata/crds/*.yaml testdata/crds
# Fetch the CRDs if not present since we need them when running fuzz tests on CI.
else
    curl -s --fail https://raw.githubusercontent.com/fluxcd/source-controller/${SOURCE_VER}/config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml -o testdata/crds/gitrepositories.yaml

    curl -s --fail https://raw.githubusercontent.com/fluxcd/image-reflector-controller/${REFLECTOR_VER}/config/crd/bases/image.toolkit.fluxcd.io_imagepolicies.yaml -o testdata/crds/imagepolicies.yaml
fi

# Using compile_go_fuzzer to compile fails when statically linking libgit2 dependencies
# via CFLAGS/CXXFLAGS.
function go_compile(){
    function=$1
    fuzzer=$2

    if [[ $SANITIZER = *coverage* ]]; then
        # ref: https://github.com/google/oss-fuzz/blob/master/infra/base-images/base-builder/compile_go_fuzzer
        compile_go_fuzzer "${PROJECT_PATH}/tests/fuzz" "${function}" "${fuzzer}"
    else
        go-fuzz -tags gofuzz -func="${function}" -o "${fuzzer}.a" .
        ${CXX} ${CXXFLAGS} ${LIB_FUZZING_ENGINE} -o "${OUT}/${fuzzer}" \
            "${fuzzer}.a" \
            "${TARGET_DIR}/lib/libgit2.a" "${TARGET_DIR}/lib/libssh2.a" \
            "${TARGET_DIR}/lib/libz.a" "${TARGET_DIR}/lib64/libssl.a" \
            "${TARGET_DIR}/lib64/libcrypto.a" \
            -fsanitize="${SANITIZER}"
    fi
}

go_compile FuzzImageUpdateReconciler fuzz_image_update_reconciler
go_compile FuzzUpdateWithSetters fuzz_update_with_setters

# By now testdata is embedded in the binaries and no longer needed.
rm -rf testdata/

popd
popd
