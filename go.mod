module github.com/fluxcd/image-automation-controller

go 1.14

require (
	github.com/fluxcd/image-reflector-controller v0.0.0-20200810165546-c2265d9b49b9
	// If you bump this, change TOOLKIT_VERSION in the Makefile to match
	github.com/fluxcd/source-controller v0.0.7
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

//  https://github.com/sosedoff/gitkit/pull/21
replace github.com/sosedoff/gitkit => github.com/hiddeco/gitkit v0.2.1-0.20200422093229-4355fec70348
