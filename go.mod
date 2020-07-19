module github.com/squaremo/image-automation-controller

go 1.13

require (
	github.com/fluxcd/pkg v0.0.3
	github.com/fluxcd/source-controller v0.0.5
	github.com/go-git/go-git/v5 v5.1.0
	github.com/go-logr/logr v0.1.0
	github.com/google/go-containerregistry v0.1.1
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/squaremo/image-reflector-controller v0.0.0-20200719062427-4f918bf22db6
	k8s.io/api v0.18.4
	k8s.io/apimachinery v0.18.4
	k8s.io/client-go v0.18.4
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/kustomize/kyaml v0.4.1
)
