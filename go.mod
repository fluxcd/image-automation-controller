module github.com/squaremo/image-automation-controller

go 1.13

require (
	github.com/fluxcd/source-controller v0.0.6
	github.com/go-git/go-git/v5 v5.1.0
	github.com/go-logr/logr v0.1.0
	github.com/google/go-containerregistry v0.1.1
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/squaremo/image-reflector-controller v0.0.0-20200719062427-4f918bf22db6
	k8s.io/api v0.18.4
	k8s.io/apimachinery v0.18.4
	k8s.io/client-go v0.18.4
	sigs.k8s.io/controller-runtime v0.6.1
	sigs.k8s.io/kustomize/kyaml v0.4.1
)
