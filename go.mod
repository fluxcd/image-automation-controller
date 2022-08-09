module github.com/fluxcd/image-automation-controller

go 1.18

replace github.com/fluxcd/image-automation-controller/api => ./api

// A temporary fork of git2go was created to enable use
// of libgit2 without thread support to fix:
// fluxcd/image-automation-controller/#339.
//
// This can be removed once libgit2/git2go#918 is merged.
//
// The fork automatically releases new patches based on upstream:
// https://github.com/pjbgf/git2go/commit/d72e39cdc20f7fe014ba73072b01ba7b569e9253
replace github.com/libgit2/git2go/v33 => github.com/pjbgf/git2go/v33 v33.0.9-nothread-check

require (
	github.com/Masterminds/sprig/v3 v3.2.2
	github.com/ProtonMail/go-crypto v0.0.0-20220730123233-d6ffb7692adf
	github.com/cyphar/filepath-securejoin v0.2.3
	github.com/fluxcd/image-automation-controller/api v0.0.0 // redundant based on replace above
	github.com/fluxcd/image-reflector-controller/api v0.20.0
	github.com/fluxcd/pkg/apis/acl v0.0.3
	github.com/fluxcd/pkg/apis/meta v0.14.2
	github.com/fluxcd/pkg/gittestserver v0.6.0
	github.com/fluxcd/pkg/runtime v0.16.2
	github.com/fluxcd/pkg/ssh v0.5.0
	github.com/fluxcd/source-controller v0.26.0
	github.com/fluxcd/source-controller/api v0.26.0
	github.com/go-logr/logr v1.2.3
	github.com/google/go-containerregistry v0.11.0
	github.com/libgit2/git2go/v33 v33.0.9
	github.com/onsi/gomega v1.20.0
	github.com/otiai10/copy v1.7.0
	github.com/spf13/pflag v1.0.5
	golang.org/x/crypto v0.0.0-20220722155217-630584e8d5aa
	k8s.io/api v0.24.3
	k8s.io/apimachinery v0.24.3
	k8s.io/client-go v0.24.3
	k8s.io/kube-openapi v0.0.0-20220803164354-a70c9af30aea
	sigs.k8s.io/controller-runtime v0.11.2
	sigs.k8s.io/kustomize/kyaml v0.13.9
)

// Fix CVE-2022-28948
replace gopkg.in/yaml.v3 => gopkg.in/yaml.v3 v3.0.1

// Fix CVE-2022-1996 (for v2, Go Modules incompatible)
replace github.com/emicklei/go-restful => github.com/emicklei/go-restful v2.16.0+incompatible

// Fix CVE-2022-1996
replace github.com/emicklei/go-restful/v3 => github.com/emicklei/go-restful/v3 v3.8.0

require (
	cloud.google.com/go/compute v1.7.0 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.1.1 // indirect
	github.com/Microsoft/go-winio v0.5.2 // indirect
	github.com/acomagu/bufpipe v1.0.3 // indirect
	github.com/asaskevich/govalidator v0.0.0-20200428143746-21a406dcc535 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/cloudflare/circl v1.1.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.8.0 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/fluxcd/gitkit v0.6.0 // indirect
	github.com/fluxcd/pkg/gitutil v0.1.0 // indirect
	github.com/fluxcd/pkg/version v0.1.0 // indirect
	github.com/fsnotify/fsnotify v1.5.1 // indirect
	github.com/go-errors/errors v1.0.1 // indirect
	github.com/go-git/gcfg v1.5.0 // indirect
	github.com/go-git/go-billy/v5 v5.3.1 // indirect
	github.com/go-git/go-git/v5 v5.4.2 // indirect
	github.com/go-logr/zapr v1.2.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/swag v0.21.1 // indirect
	github.com/gofrs/uuid v4.2.0+incompatible // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v0.12.0 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.1 // indirect
	github.com/huandu/xstrings v1.3.2 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matryer/is v1.4.0 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.4.3 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.12.2 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/sergi/go-diff v1.2.0 // indirect
	github.com/shopspring/decimal v1.2.0 // indirect
	github.com/spf13/cast v1.4.1 // indirect
	github.com/spf13/cobra v1.5.0 // indirect
	github.com/xanzy/ssh-agent v0.3.1 // indirect
	github.com/xlab/treeprint v1.1.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/net v0.0.0-20220708220712-1185a9018129 // indirect
	golang.org/x/oauth2 v0.0.0-20220718184931-c8730f7fcb92 // indirect
	golang.org/x/sys v0.0.0-20220715151400-c0bba94af5f8 // indirect
	golang.org/x/term v0.0.0-20220526004731-065cf7ba2467 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/time v0.0.0-20220411224347-583f2d630306 // indirect
	gomodules.xyz/jsonpatch/v2 v2.2.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.24.2 // indirect
	k8s.io/component-base v0.24.2 // indirect
	k8s.io/klog/v2 v2.60.1 // indirect
	k8s.io/utils v0.0.0-20220210201930-3a6ce19ff2f9 // indirect
	sigs.k8s.io/cli-utils v0.31.2 // indirect
	sigs.k8s.io/json v0.0.0-20220525155127-227cbc7cc124 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
