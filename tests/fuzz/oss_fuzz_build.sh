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

LIBGIT2_TAG="${LIBGIT2_TAG:-v0.2.0}"
GOPATH="${GOPATH:-/root/go}"
GO_SRC="${GOPATH}/src"
PROJECT_PATH="github.com/fluxcd/image-automation-controller"
TMP_DIR=$(mktemp -d /tmp/oss_fuzz-XXXXXX)

cleanup(){
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

install_deps(){
	if ! command -v go-118-fuzz-build &> /dev/null || ! command -v addimport &> /dev/null; then
		mkdir -p "${TMP_DIR}/go-118-fuzz-build"

		git clone https://github.com/AdamKorcz/go-118-fuzz-build "${TMP_DIR}/go-118-fuzz-build"
		cd "${TMP_DIR}/go-118-fuzz-build"
		go build -o "${GOPATH}/bin/go-118-fuzz-build"

		cd addimport
		go build -o "${GOPATH}/bin/addimport"
	fi

	if ! command -v goimports &> /dev/null; then
		go install golang.org/x/tools/cmd/goimports@latest
	fi
}

# Removes the content of test funcs which could cause the Fuzz
# tests to break.
remove_test_funcs(){
	filename=$1

	echo "removing co-located *testing.T"
	sed -i -e '/func Test.*testing.T) {$/ {:r;/\n}/!{N;br}; s/\n.*\n/\n/}' "${filename}"
	# Remove gomega reference as it is not used by Fuzz tests.
	sed -i 's;. "github.com/onsi/gomega";;g' "${filename}"

	# After removing the body of the go testing funcs, consolidate the imports.
	goimports -w "${filename}"
}

install_deps

cd "${GO_SRC}/${PROJECT_PATH}"

export TARGET_DIR="$(/bin/pwd)/build/libgit2/${LIBGIT2_TAG}"

# For most cases, libgit2 will already be present.
# The exception being at the oss-fuzz integration.
if [ ! -d "${TARGET_DIR}" ]; then
    curl -o output.tar.gz -LO "https://github.com/fluxcd/golang-with-libgit2/releases/download/${LIBGIT2_TAG}/linux-$(uname -m)-libgit2-only.tar.gz"

    DIR=linux-libgit2-only
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

go get github.com/AdamKorcz/go-118-fuzz-build/utils

# Setup files to be embedded into controllers_fuzzer.go's testFiles variable.
mkdir -p controllers/testdata/crd
cp config/crd/bases/*.yaml controllers/testdata/crd

export CGO_ENABLED=1
export LIBRARY_PATH="${TARGET_DIR}/lib"
export PKG_CONFIG_PATH="${TARGET_DIR}/lib/pkgconfig"
export CGO_CFLAGS="-I${TARGET_DIR}/include"
export CGO_LDFLAGS="$(pkg-config --libs --static --cflags libgit2)"

# Version of the source-controller from which to get the GitRepository CRD.
# Pulls source-controller/api's version set in go.mod.
SOURCE_VER=$(go list -m github.com/fluxcd/source-controller/api | awk '{print $2}')

# Version of the image-reflector-controller from which to get the ImagePolicy CRD.
# Pulls image-reflector-controller/api's version set in go.mod.
REFLECTOR_VER=$(go list -m github.com/fluxcd/image-reflector-controller/api | awk '{print $2}')

if [ -d "../../controllers/testdata/crds" ]; then
    cp ../../controllers/testdata/crds/*.yaml testdata/crds
else
    # Fetch the CRDs if not present since we need them when running fuzz tests on CI.
    curl -s --fail https://raw.githubusercontent.com/fluxcd/source-controller/${SOURCE_VER}/config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml -o controllers/testdata/crd/gitrepositories.yaml
    curl -s --fail https://raw.githubusercontent.com/fluxcd/image-reflector-controller/${REFLECTOR_VER}/config/crd/bases/image.toolkit.fluxcd.io_imagepolicies.yaml -o controllers/testdata/crd/imagepolicies.yaml
fi

export ADDITIONAL_LIBS="${TARGET_DIR}/lib/libgit2.a"

# Iterate through all Go Fuzz targets, compiling each into a fuzzer.
test_files=$(grep -r --include='**_test.go' --files-with-matches 'func Fuzz' .)
for file in ${test_files}
do
	remove_test_funcs "${file}"

	targets=$(grep -oP 'func \K(Fuzz\w*)' "${file}")
	for target_name in ${targets}
	do
		fuzzer_name=$(echo "${target_name}" | tr '[:upper:]' '[:lower:]')
		target_dir=$(dirname "${file}")

		echo "Building ${file}.${target_name} into ${fuzzer_name}"
		compile_native_go_fuzzer "${target_dir}" "${target_name}" "${fuzzer_name}"
	done
done
