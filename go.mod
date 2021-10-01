module github.com/fluxcd/image-automation-controller

go 1.16

replace github.com/fluxcd/image-automation-controller/api => ./api

require (
	github.com/ProtonMail/go-crypto v0.0.0-20210428141323-04723f9f07d7
	github.com/cyphar/filepath-securejoin v0.2.2
	github.com/fluxcd/image-automation-controller/api v0.0.0-00010101000000-000000000000
	// If you bump this, change REFLECTOR_VER in the Makefile to match
	github.com/fluxcd/image-reflector-controller/api v0.11.2-0.20210831115249-143780f2c726
	github.com/fluxcd/pkg/apis/meta v0.11.0-rc.1
	github.com/fluxcd/pkg/gittestserver v0.3.2
	github.com/fluxcd/pkg/runtime v0.13.0-rc.3
	github.com/fluxcd/pkg/ssh v0.1.0
	// If you bump this, change SOURCE_VER in the Makefile to match
	github.com/fluxcd/source-controller v0.15.4-0.20210812121231-7c95db88f781
	github.com/fluxcd/source-controller/api v0.15.4-0.20210812121231-7c95db88f781
	github.com/go-git/go-billy/v5 v5.3.1
	github.com/go-git/go-git/v5 v5.4.2
	github.com/go-logr/logr v0.4.0
	github.com/google/go-containerregistry v0.6.0
	github.com/libgit2/git2go/v31 v31.4.14
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.15.0
	github.com/otiai10/copy v1.2.0
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/kube-openapi v0.0.0-20210421082810-95288971da7e
	sigs.k8s.io/controller-runtime v0.10.1
	sigs.k8s.io/kustomize/kyaml v0.10.21
)

// side-effect of depending on source-controller
// required by https://github.com/helm/helm/blob/v3.6.0/go.mod
replace github.com/docker/distribution => github.com/docker/distribution v0.0.0-20191216044856-a8371794149d
