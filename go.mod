module github.com/fluxcd/image-automation-controller

go 1.18

replace github.com/fluxcd/image-automation-controller/api => ./api

// Flux has its own git2go fork to enable changes in behaviour for improved
// reliability.
//
// For more information refer to:
// - fluxcd/image-automation-controller/#339.
// - libgit2/git2go#918.
replace github.com/libgit2/git2go/v34 => github.com/fluxcd/git2go/v34 v34.0.0

// Replace by named version before merging into main.
replace github.com/fluxcd/pkg/git/libgit2 => github.com/fluxcd/pkg/git/libgit2 v0.0.0-20221007164102-c0aed7d985a4

// This lets us use `go-billy/util.Walk()`, as this function hasn't been released
// in a tagged version yet:
// https://github.com/go-git/go-billy/blob/e0768be422ff616fc042d1d62bfa65962f716ad8/util/walk.go#L59
replace github.com/go-git/go-billy/v5 => github.com/go-git/go-billy/v5 v5.3.2-0.20210603175951-e0768be422ff

require (
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20220903154154-e8044f6e4c72
	github.com/Masterminds/sprig/v3 v3.2.2
	github.com/ProtonMail/go-crypto v0.0.0-20220824120805-4b6e5c587895
	github.com/cyphar/filepath-securejoin v0.2.3
	github.com/fluxcd/image-automation-controller/api v0.26.0
	github.com/fluxcd/image-reflector-controller/api v0.22.0
	github.com/fluxcd/pkg/apis/acl v0.1.0
	github.com/fluxcd/pkg/apis/meta v0.16.0
	github.com/fluxcd/pkg/git v0.6.1
	github.com/fluxcd/pkg/git/gogit v0.1.1-0.20220902101857-4d204a4a6fa4
	github.com/fluxcd/pkg/git/libgit2 v0.1.1-0.20220908131225-538bbcd1fc66
	github.com/fluxcd/pkg/gittestserver v0.7.0
	github.com/fluxcd/pkg/runtime v0.19.0
	github.com/fluxcd/pkg/ssh v0.6.0
	github.com/fluxcd/source-controller/api v0.30.0
	github.com/go-git/go-billy/v5 v5.3.1
	github.com/go-git/go-git/v5 v5.4.2
	github.com/go-logr/logr v1.2.3
	github.com/google/go-containerregistry v0.11.0
	github.com/libgit2/git2go/v34 v34.0.0
	github.com/onsi/gomega v1.20.2
	github.com/otiai10/copy v1.7.0
	github.com/spf13/pflag v1.0.5
	golang.org/x/crypto v0.0.0-20220926161630-eccd6366d1be
	k8s.io/api v0.25.2
	k8s.io/apimachinery v0.25.2
	k8s.io/client-go v0.25.2
	k8s.io/kube-openapi v0.0.0-20220928191237-829ce0c27909
	sigs.k8s.io/controller-runtime v0.13.0
	sigs.k8s.io/kustomize/kyaml v0.13.9
)

// Fix CVE-2022-1996 (for v2, Go Modules incompatible)
replace github.com/emicklei/go-restful => github.com/emicklei/go-restful v2.16.0+incompatible

require (
	cloud.google.com/go/compute v1.7.0 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.1.1 // indirect
	github.com/Microsoft/go-winio v0.5.2 // indirect
	github.com/acomagu/bufpipe v1.0.3 // indirect
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/cloudflare/circl v1.1.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.8.0 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/fluxcd/gitkit v0.6.0 // indirect
	github.com/fluxcd/pkg/gitutil v0.2.0 // indirect
	github.com/fluxcd/pkg/http/transport v0.0.1 // indirect
	github.com/fluxcd/pkg/version v0.2.0 // indirect
	github.com/fsnotify/fsnotify v1.5.4 // indirect
	github.com/go-errors/errors v1.0.1 // indirect
	github.com/go-git/gcfg v1.5.0 // indirect
	github.com/go-logr/zapr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gofrs/uuid v4.2.0+incompatible // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.2.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.1 // indirect
	github.com/huandu/xstrings v1.3.2 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.13.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/rogpeppe/go-internal v1.8.0 // indirect
	github.com/sergi/go-diff v1.2.0 // indirect
	github.com/shopspring/decimal v1.2.0 // indirect
	github.com/spf13/cast v1.5.0 // indirect
	github.com/spf13/cobra v1.5.0 // indirect
	github.com/xanzy/ssh-agent v0.3.1 // indirect
	github.com/xlab/treeprint v1.1.0 // indirect
	go.uber.org/atomic v1.10.0 // indirect
	go.uber.org/goleak v1.2.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.23.0 // indirect
	golang.org/x/net v0.0.0-20220927171203-f486391704dc // indirect
	golang.org/x/oauth2 v0.0.0-20220909003341-f21342109be1 // indirect
	golang.org/x/sys v0.0.0-20220919091848-fb04ddd9f9c8 // indirect
	golang.org/x/term v0.0.0-20220526004731-065cf7ba2467 // indirect
	golang.org/x/text v0.3.8-0.20211004125949-5bd84dd9b33b // indirect
	golang.org/x/time v0.0.0-20220722155302-e5dcc9cfc0b9 // indirect
	gomodules.xyz/jsonpatch/v2 v2.2.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.25.0 // indirect
	k8s.io/component-base v0.25.2 // indirect
	k8s.io/klog/v2 v2.80.1 // indirect
	k8s.io/utils v0.0.0-20220922133306-665eaaec4324 // indirect
	sigs.k8s.io/cli-utils v0.33.0 // indirect
	sigs.k8s.io/json v0.0.0-20220713155537-f223a00ba0e2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
