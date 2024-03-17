# Image URL to use all building/pushing image targets
IMG ?= fluxcd/image-automation-controller
# Image tag to use all building/push image targets
TAG ?= latest

# Produce CRDs that work back to Kubernetes 1.16
CRD_OPTIONS ?= crd:crdVersions=v1

# Allows for defining additional Docker buildx arguments,
# e.g. '--push'.
BUILD_ARGS ?=
# Architectures to build images for
BUILD_PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7

# Allows for defining additional Go test args, e.g. '-tags integration'.
GO_TEST_ARGS ?= -race

# Defines whether cosign verification should be skipped.
SKIP_COSIGN_VERIFICATION ?= false

# Directory with versioned, downloaded things
CACHE := cache

# Version of the source-controller from which to get the GitRepository CRD.
# Pulls source-controller/api's version set in go.mod.
SOURCE_VER ?= $(shell go list -m github.com/fluxcd/source-controller/api | awk '{print $$2}')

# Version of the image-reflector-controller from which to get the ImagePolicy CRD.
# Pulls image-reflector-controller/api's version set in go.mod.
REFLECTOR_VER ?= $(shell go list -m github.com/fluxcd/image-reflector-controller/api | awk '{print $$2}')

# Repository root based on Git metadata.
REPOSITORY_ROOT := $(shell git rev-parse --show-toplevel)
BUILD_DIR := $(REPOSITORY_ROOT)/build

# FUZZ_TIME defines the max amount of time, in Go Duration,
# each fuzzer should run for.
FUZZ_TIME ?= 1m

ifeq ($(shell uname -s),Darwin)
GO_STATIC_FLAGS=-ldflags "-s -w" -tags 'netgo,osusergo,static_build'
endif

ifeq ($(shell uname -s),Linux)
	GO_STATIC_FLAGS=-ldflags "-s -w" -tags 'netgo,osusergo,static_build'
endif

# API (doc) generation utilities
CONTROLLER_GEN_VERSION ?= v0.14.0
GEN_API_REF_DOCS_VERSION ?= e327d0730470cbd61b06300f81c5fcf91c23c113

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

TEST_CRDS := internal/controller/testdata/crds

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
test: tidy test-api test_deps generate fmt vet manifests api-docs install-envtest ## Run tests
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS) \
	go test $(GO_STATIC_FLAGS) $(GO_TEST_ARGS) ./... -coverprofile cover.out

test-api:	## Run api tests
	cd api; go test $(GO_TEST_ARGS) ./... -coverprofile cover.out

manager: generate fmt vet	## Build manager binary
	go build -o $(BUILD_DIR)/bin/manager ./main.go

run: generate fmt vet manifests	# Run against the configured Kubernetes cluster in ~/.kube/config
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
	$(GEN_CRD_API_REFERENCE_DOCS) -api-dir=./api/v1beta2 -config=./hack/api-docs/config.json -template-dir=./hack/api-docs/template -out-file=./docs/api/v1beta2/image-automation.md

tidy:	## Run go mod tidy
	cd api; rm -f go.sum; go mod tidy -compat=1.20
	rm -f go.sum; go mod tidy -compat=1.21

fmt:	## Run go fmt against code
	go fmt ./...
	cd api; go fmt ./...

vet: ## Run go vet against code
	go vet ./...
	cd api; go vet ./...


generate: controller-gen	## Generate code
	cd api; $(CONTROLLER_GEN) object:headerFile="../hack/boilerplate.go.txt" paths="./..."

docker-build:  ## Build the Docker image
	docker buildx build \
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
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION))

# Find or download gen-crd-api-reference-docs
GEN_CRD_API_REFERENCE_DOCS = $(GOBIN)/gen-crd-api-reference-docs
.PHONY: gen-crd-api-reference-docs
gen-crd-api-reference-docs:
	$(call go-install-tool,$(GEN_CRD_API_REFERENCE_DOCS),github.com/ahmetb/gen-crd-api-reference-docs@$(GEN_API_REF_DOCS_VERSION))

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

# Build fuzzers used by oss-fuzz.
fuzz-build:
	rm -rf $(shell pwd)/build/fuzz/
	mkdir -p $(shell pwd)/build/fuzz/out/

	docker build . --tag local-fuzzing:latest -f tests/fuzz/Dockerfile.builder
	docker run --rm \
		-e FUZZING_LANGUAGE=go -e SANITIZER=address \
		-e CIFUZZ_DEBUG='True' -e OSS_FUZZ_PROJECT_NAME=fluxcd \
		-v "$(shell pwd)/build/fuzz/out":/out \
		local-fuzzing:latest

# Run each fuzzer once to ensure they will work when executed by oss-fuzz.
fuzz-smoketest: fuzz-build
	docker run --rm \
		-v "$(shell pwd)/build/fuzz/out":/out \
		-v "$(shell pwd)/tests/fuzz/oss_fuzz_run.sh":/runner.sh \
		local-fuzzing:latest \
		bash -c "/runner.sh"

# Run fuzz tests for the duration set in FUZZ_TIME.
fuzz-native: 
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS) \
	FUZZ_TIME=$(FUZZ_TIME) \
		./tests/fuzz/native_go_run.sh

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

verify:
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
