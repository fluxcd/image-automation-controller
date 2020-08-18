module github.com/fluxcd/image-automation-controller

go 1.14

require (
	github.com/fluxcd/image-reflector-controller v0.0.0-20200810165546-c2265d9b49b9
	github.com/fluxcd/pkg/gittestserver v0.0.2
	// If you bump this, change SOURCE_VERSION in the Makefile to match
	github.com/fluxcd/source-controller v0.0.10
	github.com/fluxcd/source-controller/api v0.0.10
	github.com/go-git/go-billy/v5 v5.0.0
	github.com/go-git/go-git/v5 v5.1.0
	github.com/go-logr/logr v0.1.0
	github.com/google/go-containerregistry v0.1.1
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	k8s.io/api v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	sigs.k8s.io/controller-runtime v0.6.2
	sigs.k8s.io/kustomize/kyaml v0.4.1
)
