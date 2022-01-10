# Image URL to use all building/pushing image targets
IMG ?= fluxcd/image-automation-controller
# Image tag to use all building/push image targets
TAG ?= latest

# Produce CRDs that work back to Kubernetes 1.16
CRD_OPTIONS ?= crd:crdVersions=v1

# Base image used to build the Go binary
LIBGIT2_IMG ?= ghcr.io/fluxcd/golang-with-libgit2
LIBGIT2_TAG ?= libgit2-1.1.1-1

# Allows for defining additional Docker buildx arguments,
# e.g. '--push'.
BUILD_ARGS ?=
# Architectures to build images for
BUILD_PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7

# Directory with versioned, downloaded things
CACHE := cache

# Version of the source-controller from which to get the GitRepository CRD.
# Change this if you bump the source-controller/api version in go.mod.
SOURCE_VER ?= v0.20.1

# Version of the image-reflector-controller from which to get the ImagePolicy CRD.
# Change this if you bump the image-reflector-controller/api version in go.mod.
REFLECTOR_VER ?= v0.15.0

# Version of libgit2 the controller should depend on.
LIBGIT2_VERSION ?= 1.1.1

# Repository root based on Git metadata.
REPOSITORY_ROOT := $(shell git rev-parse --show-toplevel)

# libgit2 related magical paths
# These are used to determine if the target libgit2 version is already available on
# the system, or where they should be installed to
SYSTEM_LIBGIT2_VERSION := $(shell pkg-config --modversion libgit2 2>/dev/null)
LIBGIT2_PATH := $(REPOSITORY_ROOT)/hack/libgit2
LIBGIT2_LIB_PATH := $(LIBGIT2_PATH)/lib
LIBGIT2 := $(LIBGIT2_LIB_PATH)/libgit2.so.$(LIBGIT2_VERSION)

ifneq ($(LIBGIT2_VERSION),$(SYSTEM_LIBGIT2_VERSION))
	LIBGIT2_FORCE ?= 1
endif

ifeq ($(shell uname -s),Darwin)
	LIBGIT2 := $(LIBGIT2_LIB_PATH)/libgit2.$(LIBGIT2_VERSION).dylib
	HAS_BREW := $(shell brew --version 2>/dev/null)
ifdef HAS_BREW
	HAS_OPENSSL := $(shell brew --prefix openssl@1.1)
endif
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

ifeq ($(strip ${PKG_CONFIG_PATH}),)
	MAKE_PKG_CONFIG_PATH = $(LIBGIT2_LIB_PATH)/pkgconfig
else
	MAKE_PKG_CONFIG_PATH = ${PKG_CONFIG_PATH}:$(LIBGIT2_LIB_PATH)/pkgconfig
endif

ifdef HAS_OPENSSL
	MAKE_PKG_CONFIG_PATH := $(MAKE_PKG_CONFIG_PATH):$(HAS_OPENSSL)/lib/pkgconfig
endif

TEST_CRDS := controllers/testdata/crds

# Log level for `make run`
LOG_LEVEL ?= info

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

test: $(LIBGIT2) test-api test_deps generate fmt vet manifests api-docs	## Run tests
ifeq ($(shell uname -s),Darwin)
	LD_LIBRARY_PATH=$(LIBGIT2_LIB_PATH) \
	PKG_CONFIG_PATH=$(MAKE_PKG_CONFIG_PATH) \
	CGO_LDFLAGS="-Wl,-rpath,$(LIBGIT2_LIB_PATH)" \
	go test ./... -coverprofile cover.out
else
	LD_LIBRARY_PATH=$(LIBGIT2_LIB_PATH) \
	PKG_CONFIG_PATH=$(MAKE_PKG_CONFIG_PATH) \
	go test ./... -coverprofile cover.out
endif

test-api:	## Run api tests
	cd api; go test ./... -coverprofile cover.out

manager: $(LIBGIT2) generate fmt vet	## Build manager binary
ifeq ($(shell uname -s),Darwin)
	PKG_CONFIG_PATH=$(MAKE_PKG_CONFIG_PATH) \
	CGO_LDFLAGS="-Wl,-rpath,$(LIBGIT2_LIB_PATH)" \
	go build -o bin/manager main.go
else
	PKG_CONFIG_PATH=$(MAKE_PKG_CONFIG_PATH) \
	CGO_LDFLAGS="-Wl,-rpath,$(LIBGIT2_LIB_PATH)" \
	go build -o bin/manager main.go
endif


run: $(LIBGIT2) generate fmt vet manifests	# Run against the configured Kubernetes cluster in ~/.kube/config
ifeq ($(shell uname -s),Darwin)
    CGO_LDFLAGS="-Wl,-rpath,$(LIBGIT2_LIB_PATH)" \
	go run ./main.go --log-level=${LOG_LEVEL} --log-encoding=console
else
	go run ./main.go --log-level=${LOG_LEVEL} --log-encoding=console
endif

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
	$(API_REF_GEN) -api-dir=./api/v1beta1 -config=./hack/api-docs/config.json -template-dir=./hack/api-docs/template -out-file=./docs/api/image-automation.md

tidy:	## Run go mod tidy
	cd api; rm -f go.sum; go mod tidy
	rm -f go.sum; go mod tidy

fmt:	## Run go fmt against code
	go fmt ./...
	cd api; go fmt ./...

vet: $(LIBGIT2)	## Run go vet against code
ifeq ($(shell uname -s),Darwin)
	PKG_CONFIG_PATH=$(MAKE_PKG_CONFIG_PATH) \
	CGO_LDFLAGS="-Wl,-rpath,$(LIBGIT2_LIB_PATH)" \
	go vet ./...
	cd api; go vet ./...
else
	PKG_CONFIG_PATH=$(MAKE_PKG_CONFIG_PATH) \
	go vet ./...
	cd api; go vet ./...
endif


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

controller-gen: 	## Find or download controller-gen
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.7.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

gen-crd-api-reference-docs:	## Find or download gen-crd-api-reference-docs
ifeq (, $(shell which gen-crd-api-reference-docs))
	@{ \
	set -e ;\
	API_REF_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$API_REF_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get github.com/ahmetb/gen-crd-api-reference-docs@v0.3.0 ;\
	rm -rf $$API_REF_GEN_TMP_DIR ;\
	}
API_REF_GEN=$(GOBIN)/gen-crd-api-reference-docs
else
API_REF_GEN=$(shell which gen-crd-api-reference-docs)
endif

libgit2: $(LIBGIT2)  ## Detect or download libgit2 library

$(LIBGIT2):
ifeq (1, $(LIBGIT2_FORCE))
	@{ \
	set -e; \
	mkdir -p $(LIBGIT2_PATH); \
	curl -sL https://raw.githubusercontent.com/fluxcd/golang-with-libgit2/$(LIBGIT2_TAG)/hack/Makefile -o $(LIBGIT2_PATH)/Makefile; \
	INSTALL_PREFIX=$(LIBGIT2_PATH) make -C $(LIBGIT2_PATH) libgit2; \
	}
endif

.PHONY: help
help:  ## Display this help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
