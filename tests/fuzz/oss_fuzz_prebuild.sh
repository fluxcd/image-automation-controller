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

# This file is executed by upstream oss-fuzz for any requirements that
# are specific for building this project.

# Some tests requires embedded resources. Embedding does not allow
# for traversing into ascending dirs, therefore we copy those contents here:
mkdir -p internal/controller/testdata/crd
cp config/crd/bases/*.yaml internal/controller/testdata/crd

# Version of the source-controller from which to get the GitRepository CRD.
# Pulls source-controller/api's version set in go.mod.
SOURCE_VER=$(go list -m github.com/fluxcd/source-controller/api | awk '{print $2}')

# Version of the image-reflector-controller from which to get the ImagePolicy CRD.
# Pulls image-reflector-controller/api's version set in go.mod.
REFLECTOR_VER=$(go list -m github.com/fluxcd/image-reflector-controller/api | awk '{print $2}')

if [ -d "../../internal/controller/testdata/crds" ]; then
    cp ../../internal/controller/testdata/crds/*.yaml testdata/crds
else
    # Fetch the CRDs if not present since we need them when running fuzz tests on CI.
    curl -s --fail https://raw.githubusercontent.com/fluxcd/source-controller/${SOURCE_VER}/config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml -o internal/controller/testdata/crd/gitrepositories.yaml
    curl -s --fail https://raw.githubusercontent.com/fluxcd/image-reflector-controller/${REFLECTOR_VER}/config/crd/bases/image.toolkit.fluxcd.io_imagepolicies.yaml -o internal/controller/testdata/crd/imagepolicies.yaml
fi
