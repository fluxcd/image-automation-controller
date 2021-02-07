module github.com/fluxcd/image-automation-controller

go 1.15

replace github.com/fluxcd/image-automation-controller/api => ./api

// Force downgrade to version used by kyaml.
replace gopkg.in/yaml.v3 => gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c

require (
	github.com/fluxcd/image-automation-controller/api v0.4.0
	github.com/fluxcd/image-reflector-controller/api v0.4.0
	github.com/fluxcd/pkg/apis/meta v0.7.0
	github.com/fluxcd/pkg/gittestserver v0.1.0
	github.com/fluxcd/pkg/runtime v0.8.1
	github.com/fluxcd/source-controller v0.7.0
	// If you bump this, change SOURCE_VER in the Makefile to match
	github.com/fluxcd/source-controller/api v0.7.0
	github.com/go-git/go-billy/v5 v5.0.0
	github.com/go-git/go-git/v5 v5.2.0
	github.com/go-logr/logr v0.3.0
	github.com/go-openapi/spec v0.19.5
	github.com/google/go-containerregistry v0.1.1
	github.com/libgit2/git2go/v31 v31.3.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/otiai10/copy v1.2.0
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	sigs.k8s.io/controller-runtime v0.8.0
	sigs.k8s.io/kustomize/kyaml v0.10.6
)
