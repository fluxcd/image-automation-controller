# Image URL to use all building/pushing image targets
IMG ?= fluxcd/image-automation-controller
# Image tag to use all building/push image targets
TAG ?= latest

# Produce CRDs that work back to Kubernetes 1.16
CRD_OPTIONS ?= crd:crdVersions=v1

# Base image used to build the Go binary
LIBGIT2_IMG ?= ghcr.io/fluxcd/golang-with-libgit2
LIBGIT2_TAG ?= libgit2-1.1.1-6

# Allows for defining additional Docker buildx arguments,
# e.g. '--push'.
BUILD_ARGS ?=
# Architectures to build images for
BUILD_PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7

# Directory with versioned, downloaded things
CACHE := cache

# Version of the source-controller from which to get the GitRepository CRD.
# Change this if you bump the source-controller/api version in go.mod.
SOURCE_VER ?= v0.21.1

# Version of the image-reflector-controller from which to get the ImagePolicy CRD.
# Change this if you bump the image-reflector-controller/api version in go.mod.
REFLECTOR_VER ?= v0.16.0

# Repository root based on Git metadata.
REPOSITORY_ROOT := $(shell git rev-parse --show-toplevel)
BUILD_DIR := $(REPOSITORY_ROOT)/build

# Other dependency versions
ENVTEST_BIN_VERSION ?= 1.19.2

# Caches libgit2 versions per tag, "forcing" rebuild only when needed.
LIBGIT2_PATH := $(BUILD_DIR)/libgit2/$(LIBGIT2_TAG)
LIBGIT2_LIB_PATH := $(LIBGIT2_PATH)/lib
LIBGIT2_LIB64_PATH := $(LIBGIT2_PATH)/lib64
LIBGIT2 := $(LIBGIT2_LIB_PATH)/libgit2.a
MUSL-CC =

export CGO_ENABLED=1
export PKG_CONFIG_PATH=$(LIBGIT2_LIB_PATH)/pkgconfig
export LIBRARY_PATH=$(LIBGIT2_LIB_PATH)
export CGO_CFLAGS=-I$(LIBGIT2_PATH)/include -I$(LIBGIT2_PATH)/include/openssl


# The pkg-config command will yield warning messages until libgit2 is downloaded.
ifeq ($(shell uname -s),Darwin)
export CGO_LDFLAGS=$(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) pkg-config --libs --static --cflags libssh2 openssl libgit2 2>/dev/null)
GO_STATIC_FLAGS=-ldflags "-s -w" -tags 'netgo,osusergo,static_build'
else
export PKG_CONFIG_PATH:=$(PKG_CONFIG_PATH):$(LIBGIT2_LIB64_PATH)/pkgconfig
export LIBRARY_PATH:=$(LIBRARY_PATH):$(LIBGIT2_LIB64_PATH)
export CGO_LDFLAGS=$(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) pkg-config --libs --static --cflags libssh2 openssl libgit2 2>/dev/null)
endif

ifeq ($(shell uname -s),Linux)
ifeq ($(shell uname -m),x86_64)
# Linux x86_64 seem to be able to cope with the static libraries
# by having only musl-dev installed, without the need of using musl toolchain.
	GO_STATIC_FLAGS=-ldflags "-s -w" -tags 'netgo,osusergo,static_build'
else
	MUSL-PREFIX=$(BUILD_DIR)/musl/$(shell uname -m)-linux-musl-native/bin/$(shell uname -m)-linux-musl
	MUSL-CC=$(MUSL-PREFIX)-gcc
	export CC=$(MUSL-PREFIX)-gcc
	export CXX=$(MUSL-PREFIX)-g++
	export AR=$(MUSL-PREFIX)-ar
	GO_STATIC_FLAGS=-ldflags "-s -w -extldflags \"-static\"" -tags 'netgo,osusergo,static_build'
endif
endif

# API (doc) generation utilities
CONTROLLER_GEN_VERSION ?= v0.7.0
GEN_API_REF_DOCS_VERSION ?= v0.3.0

# If gobin not set, create one on ./build and add to path.
ifeq (,$(shell go env GOBIN))
export GOBIN=$(BUILD_DIR)/gobin
else
export GOBIN=$(shell go env GOBIN)
endif
export PATH:=${GOBIN}:${PATH}

# Architecture to use envtest with
ifeq ($(shell uname -m),x86_64)
ENVTEST_ARCH ?= amd64
else
ENVTEST_ARCH ?= arm64
endif

ifeq ($(shell uname -s),Darwin)
# Envtest only supports darwin-amd64
ENVTEST_ARCH=amd64
endif

TEST_CRDS := controllers/testdata/crds

# Log level for `make run`
LOG_LEVEL ?= info

# Architecture to use envtest with
ENVTEST_ARCH ?= amd64

all: manager

# Running the tests requires the source.toolkit.fluxcd.io CRDs
test_deps: ${TEST_CRDS}/imagepolicies.yaml ${TEST_CRDS}/gitrepositories.yaml

clean_test_deps:
	rm -r ${TEST_CRDS}

${TEST_CRDS}/imagepolicies.yaml: ${CACHE}/imagepolicies_${REFLECTOR_VER}.yaml
	mkdir -p ${TEST_CRDS}
	cp $^ $@

${TEST_CRDS}/gitrepositories.yaml: ${CACHE}/gitrepositories_${SOURCE_VER}.yaml
	mkdir -p ${TEST_CRDS}
	cp $^ $@

${CACHE}/gitrepositories_${SOURCE_VER}.yaml:
	mkdir -p ${CACHE}
	curl -s --fail https://raw.githubusercontent.com/fluxcd/source-controller/${SOURCE_VER}/config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml \
		-o ${CACHE}/gitrepositories_${SOURCE_VER}.yaml

${CACHE}/imagepolicies_${REFLECTOR_VER}.yaml:
	mkdir -p ${CACHE}
	curl -s --fail https://raw.githubusercontent.com/fluxcd/image-reflector-controller/${REFLECTOR_VER}/config/crd/bases/image.toolkit.fluxcd.io_imagepolicies.yaml \
		-o ${CACHE}/imagepolicies_${REFLECTOR_VER}.yaml

check-deps:
ifeq ($(shell uname -s),Darwin)
	if ! command -v pkg-config &> /dev/null; then echo "pkg-config is required"; exit 1; fi
endif

KUBEBUILDER_ASSETS?="$(shell $(ENVTEST) --arch=$(ENVTEST_ARCH) use -i $(ENVTEST_KUBERNETES_VERSION) --bin-dir=$(ENVTEST_ASSETS_DIR) -p path)"
test: $(LIBGIT2) test-api test_deps generate fmt vet manifests api-docs	install-envtest ## Run tests
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS) \
	go test $(GO_STATIC_FLAGS) ./... -coverprofile cover.out

test-api:	## Run api tests
	cd api; go test ./... -coverprofile cover.out

manager: $(LIBGIT2) generate fmt vet	## Build manager binary
	go run ./main.go


run: $(LIBGIT2) generate fmt vet manifests	# Run against the configured Kubernetes cluster in ~/.kube/config
	go run $(GO_STATIC_FLAGS) ./main.go --log-level=${LOG_LEVEL} --log-encoding=console

install: manifests	## Install CRDs into a cluster
	kustomize build config/crd | kubectl apply -f -

uninstall: manifests	## Uninstall CRDs from a cluster
	kustomize build config/crd | kubectl delete -f -

deploy: manifests	## Deploy controller in the configured Kubernetes cluster in ~/.kube/config
	cd config/manager && kustomize edit set image fluxcd/image-automation-controller=$(IMG):$(TAG)
	kustomize build config/default | kubectl apply -f -

dev-deploy: manifests
	mkdir -p config/dev && cp config/default/* config/dev
	cd config/dev && kustomize edit set image fluxcd/image-automation-controller=$(IMG):$(TAG)
	kustomize build config/dev | kubectl apply -f -
	rm -rf config/dev

manifests: controller-gen	## Generate manifests e.g. CRD, RBAC etc.
	cd api; $(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role paths="./..." output:crd:artifacts:config="../config/crd/bases"

api-docs: gen-crd-api-reference-docs	## Generate API reference documentation
	$(GEN_CRD_API_REFERENCE_DOCS) -api-dir=./api/v1beta1 -config=./hack/api-docs/config.json -template-dir=./hack/api-docs/template -out-file=./docs/api/image-automation.md

tidy:	## Run go mod tidy
	cd api; rm -f go.sum; go mod tidy
	rm -f go.sum; go mod tidy

fmt:	## Run go fmt against code
	go fmt ./...
	cd api; go fmt ./...

vet: $(LIBGIT2)	## Run go vet against code
	go vet ./...
	cd api; go vet ./...


generate: controller-gen	## Generate code
	cd api; $(CONTROLLER_GEN) object:headerFile="../hack/boilerplate.go.txt" paths="./..."

docker-build:  ## Build the Docker image
	docker buildx build \
		--build-arg LIBGIT2_IMG=$(LIBGIT2_IMG) \
		--build-arg LIBGIT2_TAG=$(LIBGIT2_TAG) \
		--platform=$(BUILD_PLATFORMS) \
		-t $(IMG):$(TAG) \
		$(BUILD_ARGS) .

docker-push:	## Push the Docker image
	docker push $(IMG):$(TAG)

docker-deploy:	## Set the Docker image in-cluster
	kubectl -n flux-system set image deployment/image-automation-controller manager=$(IMG):$(TAG)

# Find or download controller-gen
CONTROLLER_GEN = $(GOBIN)/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.7.0)

libgit2: $(LIBGIT2)  ## Detect or download libgit2 library

$(LIBGIT2): $(MUSL-CC)
	IMG=$(LIBGIT2_IMG) TAG=$(LIBGIT2_TAG) ./hack/install-libraries.sh

$(MUSL-CC):
ifneq ($(shell uname -s),Darwin)
	./hack/download-musl.sh
endif

# Find or download gen-crd-api-reference-docs
GEN_CRD_API_REFERENCE_DOCS = $(GOBIN)/gen-crd-api-reference-docs
.PHONY: gen-crd-api-reference-docs
gen-crd-api-reference-docs:
	$(call go-install-tool,$(GEN_CRD_API_REFERENCE_DOCS),github.com/ahmetb/gen-crd-api-reference-docs@v0.3.0)

ENVTEST_ASSETS_DIR=$(BUILD_DIR)/testbin
ENVTEST_KUBERNETES_VERSION?=latest
install-envtest: setup-envtest
	mkdir -p ${ENVTEST_ASSETS_DIR}
	$(ENVTEST) use $(ENVTEST_KUBERNETES_VERSION) --arch=$(ENVTEST_ARCH) --bin-dir=$(ENVTEST_ASSETS_DIR)
	chmod -R u+w $(BUILD_DIR)/testbin

ENVTEST = $(GOBIN)/setup-envtest
.PHONY: envtest
setup-envtest: ## Download envtest-setup locally if necessary.
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-install-tool will 'go install' any package $2 and install it to $1.
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
env -i bash -c "GOBIN=$(GOBIN) PATH=$(PATH) GOPATH=$(shell go env GOPATH) GOCACHE=$(shell go env GOCACHE) go install $(2)" ;\
rm -rf $$TMP_DIR ;\
}
endef

update-attributions:
	./hack/update-attributions.sh

verify: update-attributions fmt
ifneq (, $(shell git status --porcelain --untracked-files=no))
	@{ \
	echo "working directory is dirty:"; \
	git --no-pager diff; \
	exit 1; \
	}
endif

.PHONY: help
help:  ## Display this help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
