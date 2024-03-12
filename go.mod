module github.com/fluxcd/image-automation-controller

go 1.21

replace github.com/fluxcd/image-automation-controller/api => ./api

// Pin kustomize to v5.3.0
replace (
	sigs.k8s.io/kustomize/api => sigs.k8s.io/kustomize/api v0.16.0
	sigs.k8s.io/kustomize/kyaml => sigs.k8s.io/kustomize/kyaml v0.16.0
)

require (
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20230811130428-ced1acdcaa24
	github.com/Masterminds/sprig/v3 v3.2.3
	github.com/ProtonMail/go-crypto v1.1.0-alpha.0
	github.com/cyphar/filepath-securejoin v0.2.4
	github.com/fluxcd/image-automation-controller/api v0.37.1
	github.com/fluxcd/image-reflector-controller/api v0.31.2
	github.com/fluxcd/pkg/apis/acl v0.1.0
	github.com/fluxcd/pkg/apis/event v0.7.0
	github.com/fluxcd/pkg/apis/meta v1.3.0
	github.com/fluxcd/pkg/git v0.17.0
	github.com/fluxcd/pkg/git/gogit v0.17.0
	github.com/fluxcd/pkg/gittestserver v0.10.0
	github.com/fluxcd/pkg/runtime v0.44.1
	github.com/fluxcd/pkg/ssh v0.11.0
	github.com/fluxcd/source-controller/api v1.2.4
	github.com/go-git/go-billy/v5 v5.5.0
	github.com/go-git/go-git/v5 v5.11.0
	github.com/go-logr/logr v1.3.0
	github.com/google/go-containerregistry v0.19.0
	github.com/onsi/gomega v1.31.1
	github.com/otiai10/copy v1.14.0
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.28.6
	k8s.io/apimachinery v0.28.6
	k8s.io/client-go v0.28.6
	k8s.io/kube-openapi v0.0.0-20231206194836-bf4651e18aa8
	k8s.io/utils v0.0.0-20231127182322-b307cd553661
	sigs.k8s.io/controller-runtime v0.16.3
	sigs.k8s.io/kustomize/kyaml v0.16.0
)

require (
	dario.cat/mergo v1.0.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.2.1 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/cloudflare/circl v1.3.7 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/evanphx/json-patch v5.7.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.7.0 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fluxcd/cli-utils v0.36.0-flux.3 // indirect
	github.com/fluxcd/gitkit v0.6.0 // indirect
	github.com/fluxcd/pkg/version v0.2.2 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.20.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.4 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.5.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.3.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.5 // indirect
	github.com/huandu/xstrings v1.3.3 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pjbgf/sha1cd v0.3.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sergi/go-diff v1.3.1 // indirect
	github.com/shopspring/decimal v1.2.0 // indirect
	github.com/skeema/knownhosts v1.2.1 // indirect
	github.com/spf13/cast v1.5.0 // indirect
	github.com/spf13/cobra v1.8.0 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.starlark.net v0.0.0-20231121155337-90ade8b19d09 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.26.0 // indirect
	golang.org/x/crypto v0.18.0 // indirect
	golang.org/x/exp v0.0.0-20231206192017-f3f8817b8deb // indirect
	golang.org/x/mod v0.14.0 // indirect
	golang.org/x/net v0.20.0 // indirect
	golang.org/x/oauth2 v0.16.0 // indirect
	golang.org/x/sync v0.6.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/term v0.16.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.17.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/evanphx/json-patch.v5 v5.7.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.28.6 // indirect
	k8s.io/cli-runtime v0.28.6 // indirect
	k8s.io/component-base v0.28.6 // indirect
	k8s.io/klog/v2 v2.110.1 // indirect
	k8s.io/kubectl v0.28.6 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/kustomize/api v0.16.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
