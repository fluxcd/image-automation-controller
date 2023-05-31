# Changelog

## 0.34.1

**Release date:** 2023-06-01

This prerelease fixes a regression introduced in `v0.34.0` where
support for Git servers that exclusively use v2 of the wire protocol like Azure
Devops and AWS CodeCommit was broken.
Furthermore, the reconciler now errors out if it fails to get the signing entity
to be used for Git commit signing.

Fixes:
- Return signing entity parsing error
  [#527](https://github.com/fluxcd/image-automation-controller/pull/527)
- Set controller package name
  [#529](https://github.com/fluxcd/image-automation-controller/pull/529)
- Bump `fluxcd/pkg/git/gogit` to v0.12.0
  [#530](https://github.com/fluxcd/image-automation-controller/pull/530)

## 0.34.0

**Release date:** 2023-05-29

This prerelease comes with support for Kubernetes v1.27 and updates to the
controller's dependencies.

Improvements:
- Update controller-runtime, Kubernetes and kyaml dependencies
  [#518](https://github.com/fluxcd/image-automation-controller/pull/518)
- Drop go-git fork in favor of go-git v5.7.0
  [#519](https://github.com/fluxcd/image-automation-controller/pull/519)
- Update workflows and enable dependabot
  [#520](https://github.com/fluxcd/image-automation-controller/pull/520)
- build(deps): bump github/codeql-action from 2.3.3 to 2.3.4
  [#521](https://github.com/fluxcd/image-automation-controller/pull/521)
- Update source-controller to v1.0.0-rc.4
  [#523](https://github.com/fluxcd/image-automation-controller/pull/523)

## 0.33.1

**Release date:** 2023-05-12

This prerelease comes with updates to the controller dependencies
to patch CVE-2023-1732.

In addition, the controller base image has been updated to Alpine 3.18.

Improvements:
- Update Alpine to 3.18
  [#513](https://github.com/fluxcd/image-automation-controller/pull/513)
- Update dependencies
  [#516](https://github.com/fluxcd/image-automation-controller/pull/516)
- build(deps): bump github.com/cloudflare/circl from 1.3.2 to 1.3.3
  [#515](https://github.com/fluxcd/image-automation-controller/pull/515)

## 0.33.0

**Release date:** 2023-05-09

This prerelease comes with support for signing commits using password protected OpenPGP keys.

In addition, the controller dependencies have been updated to their latest
versions.

Improvements:
- Add support for commit signing PGP key passphrases
  [#510](https://github.com/fluxcd/image-automation-controller/pull/510)
- Update dependencies
  [#511](https://github.com/fluxcd/image-automation-controller/pull/511)

## 0.32.0

**Release date:** 2023-03-31

This prerelease updates the dependencies to their latest versions.

In addition, the controller now supports horizontal scaling using sharding based on a label selector.

### Highlights

#### API Changes

This prerelease is only compatible with `GitRepository` API version `v1`, first shipped with 
[source-controller](https://github.com/fluxcd/source-controller) v1.0.0-rc.1.

#### Sharding

Starting with this release, the controller can be configured with `--watch-label-selector`, after 
which only objects with this label will be reconciled by the controller.

This allows for horizontal scaling, where image-automation-controller can be deployed multiple times
with a unique label selector which is used as the sharding key.

### Full Changelog

Improvements:
- move `controllers` to `internal/controllers`
  [#500](https://github.com/fluxcd/image-automation-controller/pull/500)
- Add reconciler sharding capability based on label selector
  [#504](https://github.com/fluxcd/image-automation-controller/pull/504)
- Update dependencies and GitRepository API to v1
  [#505](https://github.com/fluxcd/image-automation-controller/pull/505)
- bump google.golang.org/protobuf from 1.29.0 to 1.29.1
  [#506](https://github.com/fluxcd/image-automation-controller/pull/506)

## 0.31.0

**Release date:** 2023-03-08

This release updates to Go version the controller is build with to 1.20, and
updates the dependencies to their latest versions.

In addition, `klog` is now configured to log using the same logger as the rest
of the controller (providing a consistent log format).

Improvements:
- Update Go to 1.20
  [#492](https://github.com/fluxcd/image-automation-controller/pull/492)
- Update dependencies
  [#494](https://github.com/fluxcd/image-automation-controller/pull/494)
  [#496](https://github.com/fluxcd/image-automation-controller/pull/496)
- Use `logger.SetLogger` to also configure `klog`
  [#495](https://github.com/fluxcd/image-automation-controller/pull/495)

## 0.30.0

**Release date:** 2023-02-16

This prerelease comes with support for the new `ImagePolicy` v1beta2 API. See
[image-reflector-controller
changelog](https://github.com/fluxcd/image-reflector-controller/blob/v0.25.0/CHANGELOG.md#0250)
for instructions about updating the `ImagePolicies`.

:warning: Also note that the autologin flags in image-reflector-controller have
been deprecated and the `ImageRepositories` that use the autologin feature have
to be updated with `.spec.provider` field to continue working. Refer the
[docs](https://fluxcd.io/flux/components/image/imagerepositories/#provider) for
details and examples.

In addition, the controller dependencies have been updated to their latest
versions.

Improvements:
- Set rate limiter option in test reconcilers
  [#475](https://github.com/fluxcd/image-automation-controller/pull/475)
- Update dependencies
  [#486](https://github.com/fluxcd/image-automation-controller/pull/486)
- Update image-reflector API to v1beta2
  [#485](https://github.com/fluxcd/image-automation-controller/pull/485)

## 0.29.0

**Release date:** 2023-02-01

This prerelease disables caching of Secrets and ConfigMaps to improve memory
usage. To opt-out from this behavior, start the controller with:
`--feature-gates=CacheSecretsAndConfigMaps=true`.

In addition, the controller dependencies have been updated to Kubernetes
v1.26.1 and controller-runtime v0.14.2. The controller base image has been
updated to Alpine 3.17.

Improvements:
- build: Enable SBOM and SLSA Provenance
  [#478](https://github.com/fluxcd/image-automation-controller/pull/478)
- Disable caching of Secrets and ConfigMaps
  [#479](https://github.com/fluxcd/image-automation-controller/pull/479)
- Update dependencies
  [#480](https://github.com/fluxcd/image-automation-controller/pull/480)

## 0.28.0

**Release date:** 2022-12-21

This prerelease removes all code references to `libgit2` and `git2go`, from
this release onwards the controller will use `go-git` as the only git implementation.
For more information, refer to version 0.27.0's changelog, which started `libgit2`'s
deprecation process.

The feature gate `ForceGoGitImplementation` was removed, users passing it as their
controller's startup args will need to remove it before upgrading.

Two new feature gates were introduced and are enabled by default:
- `GitShallowClone`: enables the use of shallow clones when pulling source
from Git repositories.
- `GitAllBranchReferences`: enables users to toggle the download of all branch
head references when push branches are configured.

To opt-out from the feature gates above, start the controller with:
`--feature-gates=GitShallowClone=false,GitAllBranchReferences=false`.

Fixes:
- Block the creation of empty commits
  [#470](https://github.com/fluxcd/image-automation-controller/pull/470)

Improvements:
- Add GitShallowClone feature
  [#463](https://github.com/fluxcd/image-automation-controller/pull/463)
- Add feature gate GitAllBranchReferences
  [#469](https://github.com/fluxcd/image-automation-controller/pull/469)
- Remove libgit2 and git2go from codebase
  [#468](https://github.com/fluxcd/image-automation-controller/pull/468)
- build: Link libgit2 via LIB_FUZZING_ENGINE
  [#465](https://github.com/fluxcd/image-automation-controller/pull/465)
- build: Add postbuild script for fuzzing
  [#464](https://github.com/fluxcd/image-automation-controller/pull/464)
- build: Fix cifuzz and improve fuzz tests' reliability
  [#462](https://github.com/fluxcd/image-automation-controller/pull/462)
- Update dependencies
  [#471](https://github.com/fluxcd/image-automation-controller/pull/471)

## 0.27.0

**Release date:** 2022-11-21

This prerelease comes with a major refactoring of the controller's Git operations.
The controller can now observe the field `spec.gitImplementation` instead of always
ignoring it and using `libgit2`. The `go-git` implementation now supports all Git
servers, including Azure DevOps, which previously was only supported by `libgit2`.

By default, the field `spec.gitImplementation` is ignored and the reconciliations
will use `go-git`. To opt-out from this behaviour, and get the controller to 
honour the field `spec.gitImplementation`, start the controller with:
`--feature-gates=ForceGoGitImplementation=false`.

This version initiates the soft deprecation of the `libgit2` implementation.
The motivation for removing support for `libgit2` being:
- Reliability: over the past months we managed to substantially reduce the
issues users experienced, but there are still crashes happening when the controller
runs over longer periods of time, or when under intense GC pressure.
- Performance: due to the inherit nature of `libgit2` implementation, which
is a C library called via CGO through `git2go`, it will never perform as well as
a pure Go implementations. At scale, memory pressure insues which then triggers
the reliability issues above.
- Lack of Shallow Clone Support.
- Maintainability: supporting two Git implementations is a big task, even more
so when one of them is in a complete different tech stack. Given its nature, to
support `libgit2`, we have to maintain an additional repository. Statically built
`libgit2` libraries need to be cross-compiled for all our supported platforms.
And a lot of "unnecessary" code has to be in place to make building, testing and
fuzzing work seamlessly.

Users having any issues with `go-git` should report it to the Flux team,
so any issues can be resolved before support for `libgit2` is completely
removed from the codebase.

Starting from this version `ImageUpdateAutomation` objects with a `spec.PushBranch`
specified will have the push branch refreshed automatically via force push.
To opt-out from this behaviour, start the controller with:
`--feature-gates=GitForcePushBranch=false`.

Improvements:
- Refactor Git operations and introduce go-git support for Azure DevOps and AWS CodeCommit
  [#451](https://github.com/fluxcd/image-automation-controller/pull/451)
- Add new ForceGoGitImplementation FeatureGate
  [#452](https://github.com/fluxcd/image-automation-controller/pull/452)
- Add support for force push
  [#453](https://github.com/fluxcd/image-automation-controller/pull/453)
- Use Flux Event API v1beta1
  [#455](https://github.com/fluxcd/image-automation-controller/pull/455)
- Remove deprecated alpha APIs 
  [#456](https://github.com/fluxcd/image-automation-controller/pull/456)
- Remove nsswitch.conf creation
  [#458](https://github.com/fluxcd/image-automation-controller/pull/458)
- Update Dependencies
  [#459](https://github.com/fluxcd/image-automation-controller/pull/459)
  [#460](https://github.com/fluxcd/image-automation-controller/pull/460)

## 0.26.1

**Release date:** 2022-10-21

This prerelease comes with dependency updates, including the fix for the upstream
vulnerability CVE-2022-32149.

In addition, the controller dependencies have been updated to Kubernetes v1.25.3.

Improvements:
- Update dependencies
  [#448](https://github.com/fluxcd/image-automation-controller/pull/448)

## 0.26.0

**Release date:** 2022-09-29

This prerelease comes with strict validation rules for API fields which define a
(time) duration. Effectively, this means values without a time unit (e.g. `ms`,
`s`, `m`, `h`) will now be rejected by the API server. To stimulate sane
configurations, the units `ns`, `us` and `µs` can no longer be configured, nor
can `h` be set for fields defining a timeout value.

In addition, the controller dependencies have been updated
to Kubernetes controller-runtime v0.13.

:warning: **Breaking changes:**
- `.spec.interval` new validation pattern is `"^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"`

Improvements:
* api: add custom validation for v1.Duration types
  [#439](https://github.com/fluxcd/image-automation-controller/pull/439)
* Update dependencies
  [#442](https://github.com/fluxcd/image-automation-controller/pull/442)
  [#444](https://github.com/fluxcd/image-automation-controller/pull/444)
* Build with Go 1.19
  [#440](https://github.com/fluxcd/image-automation-controller/pull/440)
* build: Bump CI to macos-11
  [#441](https://github.com/fluxcd/image-automation-controller/pull/441)
* Fix build by enabling Cosign experimental
  [#438](https://github.com/fluxcd/image-automation-controller/pull/438)

## 0.25.0

**Release date:** 2022-09-12

This prerelease comes with improvements to fuzzing.
In addition, the controller dependencies have been updated
to Kubernetes controller-runtime v0.12.

:warning: **Breaking change:** The controller logs have been aligned
with the Kubernetes structured logging. For more details on the new logging
structure please see: [fluxcd/flux2#3051](https://github.com/fluxcd/flux2/issues/3051).

Improvements:
- Align controller logs to Kubernetes structured logging
  [#429](https://github.com/fluxcd/image-automation-controller/pull/429)
- Align output with source-controller on no-ops
  [#431](https://github.com/fluxcd/image-automation-controller/pull/431)
- Refactor Fuzzers based on Go native fuzzing
  [#432](https://github.com/fluxcd/image-automation-controller/pull/432)
- Update dependencies
  [#434](https://github.com/fluxcd/image-automation-controller/pull/434)

## 0.24.2

**Release date:** 2022-08-29

This prerelease comes with a bug fix for when the push branch and reference branch are equal.

In addition, the controller dependencies have been updated to Kubernetes v1.25.0.

Fixes:
- Fix fetch error in push branch
  [#423](https://github.com/fluxcd/image-automation-controller/pull/423)

Improvements:
- Update Kubernetes packages to v1.25.0
  [#425](https://github.com/fluxcd/image-automation-controller/pull/425)
- fuzz: Ensure Go 1.18 for fuzz image
  [#424](https://github.com/fluxcd/image-automation-controller/pull/424)

## 0.24.1

**Release date:** 2022-08-10

This prerelease comes with panic recovery, to protect the controller from
crashing when reconciliations lead to a crash.

Improvements:
- Enable RecoverPanic
  [#416](https://github.com/fluxcd/image-automation-controller/pull/416)

## 0.24.0

**Release date:** 2022-08-09

This prerelease comes with internal changes that improve the way the controller
interacts with Git repositories and some improvement on error messages. 

Unmanaged Transport is no longer supported and has now been decommissioned across
Flux controllers. 

In some instances when Flux is trying to push changes using read-only keys the
controller would return an `early EOF` error message. This has now been changed
to `early EOF (the SSH key may not have write access to the repository)`.

Improvements:
- Enrich early EOF error message
  [#410](https://github.com/fluxcd/image-automation-controller/pull/410)
- Remove MUSL and enable threadless libgit2 support
  [#411](https://github.com/fluxcd/image-automation-controller/pull/411)
- Decommission libgit2 Unmanaged Transport
  [#412](https://github.com/fluxcd/image-automation-controller/pull/412)

## 0.23.5

**Release date:** 2022-07-15

This prerelease comes with some minor improvements and update dependencies to patch upstream CVEs.

Improvements:
- Update dependencies
  [#408](https://github.com/fluxcd/image-automation-controller/pull/408)
  [#401](https://github.com/fluxcd/image-automation-controller/pull/401)
- build: Upgrade to Go 1.18
  [#403](https://github.com/fluxcd/image-automation-controller/pull/403)
- build: provenance and tampering checks for libgit2
  [#406](https://github.com/fluxcd/image-automation-controller/pull/406)
- Update libgit2 to v1.3.2
  [#407](https://github.com/fluxcd/image-automation-controller/pull/407)

## 0.23.4

**Release date:** 2022-06-24

This prerelease comes with finalizer in `ImageUpdateAutomation` resource, which
helps ensure that the resource deletion is recorded in the metrics properly.
source-controller dependency was updated to `v0.25.8` which fixes an
authentication issue when using libgit2 managed transport to checkout
repositories on Bitbucket server.

In addition, `github.com/emicklei/go-restful` was also updated to `v3.8.0` to
please static analysers and fix warnings for CVE-2022-1996.

Improvements:
- Update source-controller and image-reflector-controller
  [#399](https://github.com/fluxcd/image-automation-controller/pull/399)
- Update go-restful to v3.8.0 (Fix CVE-2022-1996)
  [#398](https://github.com/fluxcd/image-automation-controller/pull/398)
- Add finalizer to ImageUpdateAutomation resources
  [#397](https://github.com/fluxcd/image-automation-controller/pull/397)

## 0.23.3

**Release date:** 2022-06-22

This prerelease adds a new flag for configuring the SSH host key algorithms and
updates the source-controller dependency to v0.25.7, which fixes a deadlock
scenario in the SSH managed transport and some SSH connection leak issues.

Improvements:
- Update source-controller to v0.25.7
  [#393](https://github.com/fluxcd/image-automation-controller/pull/393)
- build: enable -race for go test
  [#389](https://github.com/fluxcd/image-automation-controller/pull/389)
- Add new flag --ssh-hostkey-algos
  [#388](https://github.com/fluxcd/image-automation-controller/pull/388)

## 0.23.2

**Release date:** 2022-06-08

This prerelease fixes a regression for SSH host key verification and
updates some dependencies.

In addition, the controller was updated to Kubernetes v1.24.1.

Fixes:
- Update github.com/fluxcd/source-controller v0.25.5
  [#384](https://github.com/fluxcd/image-automation-controller/pull/384)

Improvements:
- Update dependencies
  [#383](https://github.com/fluxcd/image-automation-controller/pull/383)

## 0.23.1

**Release date:** 2022-06-07

This prerelease fixes a regression when accessing Gitlab via HTTPS
when the URL does not have the '.git' suffix and updates dependencies. 

Improvements:
- Update dependencies and source-controller to v0.25.4
  [#380](https://github.com/fluxcd/image-automation-controller/pull/380)
- Update source-controller/api to v0.25.2
  [#377](https://github.com/fluxcd/image-automation-controller/pull/377)

## 0.23.0

**Release date:** 2022-06-02

This prerelease comes with a new flag `--feature-gate` to disable/enable
experimental features. It works in a similar manner to [Kubernetes feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/).

The libgit2 managed transport feature has been enabled by default. Furthermore,
a few changes have been made to make the feature more stable and enable quicker
clones. Users that want to opt out and use the unmanaged transports may do so
by passing the flag `--feature-gate=GitManagedTransport=false`, but please note
that we encourage users not to do so.

A regression that was introduced in PR [#330](https://github.com/fluxcd/image-automation-controller/pull/330)
which made the controller fail when it tried to push commits to a branch that
already existed on remote, has been fixed.

Improvements:
- Update dependencies
  [#368](https://github.com/fluxcd/image-automation-controller/pull/368)
- Enable Managed Transport by default
  [#369](https://github.com/fluxcd/image-automation-controller/pull/369)
- Update dependencies
  [#374](https://github.com/fluxcd/image-automation-controller/pull/374)
- Update source-controller with libgit2 race fixes
  [#376](https://github.com/fluxcd/image-automation-controller/pull/376)

Fixes:
- Instruct kyaml/kio to retain sequence indentation style
  [#366](https://github.com/fluxcd/image-automation-controller/pull/366)
- git: refactor tests to use managed transports and fix switchToBranch
  [#374](https://github.com/fluxcd/image-automation-controller/pull/374)

## 0.22.1

**Release date:** 2022-05-03

This prerelease comes with dependency updates, including an update of
`image-reflector-controller` to `v0.18.0`.

Improvements:
- Update dependencies
  [#359](https://github.com/fluxcd/image-automation-controller/pull/359)

Other notable changes:
- Rewrite all the tests to testenv with gomega
  [#356](https://github.com/fluxcd/image-automation-controller/pull/356)

## 0.22.0

**Release date:** 2022-04-19

This prerelease comes with further stability improvements in the `libgit2`
experimental management transport, brings ways to configure Key Exchange
Algorithms, plus some extra housekeeping awesomeness.

Managed Transport for `libgit2` now introduces self-healing capabilities,
to recover from failure when long-running connections become stale.

The Key Exchange Algorithms used when establishing SSH connections are
based on the defaults configured upstream in `go-git` and `golang.org/x/crypto`.
Now this can be overriden with the flag `--ssh-kex-algos`. Note this applies
to the `go-git` gitImplementation or the `libgit2` gitImplementation but
_only_ when Managed Transport is being used.

The exponential back-off retry can be configured with the new flags:
`--min-retry-delay` (default: `750ms`) and `--max-retry-delay`
(default: `15min`). Previously the defaults were set to `5ms` and `1000s`,
which in some cases impaired the controller's ability to self-heal 
(e.g. retrying failing SSH connections).

Improvements:
- Update source controller to improve managed transport
  [#346](https://github.com/fluxcd/image-automation-controller/pull/346)
- Add flags to configure exponential back-off retry
  [#348](https://github.com/fluxcd/image-automation-controller/pull/348)
- Update libgit2 to 1.3.1
  [#350](https://github.com/fluxcd/image-automation-controller/pull/350)
- Add flag to allow configuration of ssh kex algos
  [#351](https://github.com/fluxcd/image-automation-controller/pull/351)
- Update dependencies
  [#352](https://github.com/fluxcd/image-automation-controller/pull/352)
  [#353](https://github.com/fluxcd/image-automation-controller/pull/353)
  [#354](https://github.com/fluxcd/image-automation-controller/pull/354)

## 0.21.3

**Release date:** 2022-03-30

This prerelease comes with general stability improvements in the libgit2
experimental management transport and updates various dependencies to their
latest versions.

Improvements:
- Update dependencies
  [#340](https://github.com/fluxcd/image-automation-controller/pull/340)

## 0.21.2

**Release date:** 2022-03-28

This prerelease improves on the experimental managed transport's overall
stability. Changes of note:
- SSH connections now being reused across git operations.
- Leaked HTTP connections are now fixed.
- The long-standing SSH intermittent errors are addressed by the cached connections.

Fixes:
- Update source controller to v0.22.4
  [#337](https://github.com/fluxcd/image-automation-controller/pull/337)

## 0.21.1

**Release date:** 2022-03-23

This prerelease fixes a bug introduced in `v0.21.0`, where pushes into a branch
different from the origin branch would squash all historical commits into one.

In addition, it ensures the API objects fully adhere to newly introduced
interfaces, allowing them to work in combination with e.g. the
[`conditions`](https://pkg.go.dev/github.com/fluxcd/pkg/runtime@v0.13.2/conditions)
package.

Improvements:
- Implement `meta.ObjectWithConditions` interfaces
  [#328](https://github.com/fluxcd/image-automation-controller/pull/328)
- Update image-reflector-controller API to v0.17.1
  [#331](https://github.com/fluxcd/image-automation-controller/pull/331)
- Update source-controller to v0.22.2
  [#333](https://github.com/fluxcd/image-automation-controller/pull/333)

Fixes:
- Fix bug when pushing into different branch
  [#330](https://github.com/fluxcd/image-automation-controller/pull/330)

## 0.21.0

**Release date:** 2022-03-22

This prerelease further improves Git operations' stability, upgrades source
controller to `v0.22` and prepares the code base for more standardized 
controller runtime operations.

The source-controller dependency was updated to version `v0.22` which 
introduces API `v1beta2`. This requires the source-controller running in
the same cluster to be `v0.22.0` or greater.

Git operations using `go-git` have been migrated away to `git2go`, which is now
the only framework this controller uses for interacting with repositories.

A new experimental transport has been added to improve reliability, adding
timeout enforcement for Git network operations.
Opt-in by setting the environment variable `EXPERIMENTAL_GIT_TRANSPORT` to
`true` in the controller's Deployment. This will result in the low-level
transport being handled by the controller, instead of `libgit2`. It may result
in an increased number of timeout messages in the logs, however it will resolve
the bug in which Git operations can make the controllers hang indefinitely.

Improvements:
* Update libgit2 to 1.3.0
  [#321](https://github.com/fluxcd/image-automation-controller/pull/321)
* Remove direct dependency to go-git
  [#324](https://github.com/fluxcd/image-automation-controller/pull/324)
* Update `pkg/runtime` and `apis/meta`
  [#325](https://github.com/fluxcd/image-automation-controller/pull/325)
* Add experimental managed transport for libgit2 operations
  [#326](https://github.com/fluxcd/image-automation-controller/pull/326)

Fixes:
* Update libgit2 to 1.3.0
  [#320](https://github.com/fluxcd/image-automation-controller/issues/320)
* Consolidate use of libgit2 for git operations
  [#323](https://github.com/fluxcd/image-automation-controller/issues/323)
* unable to clone: Certificate
  [#298](https://github.com/fluxcd/image-automation-controller/issues/298)
* Controller stops reconciling, needs restart
  [#282](https://github.com/fluxcd/image-automation-controller/issues/282)
* image-automation-controller not reconnecting after operation timed out
  [#209](https://github.com/fluxcd/image-automation-controller/issues/209)

## 0.20.1

**Release date:** 2022-03-01

This prerelease comes with improvements to the libgit2 OpenSSL build dependency,
which fixes some issues related to git server connection leaks.

In addition, `github.com/prometheus/client_golang` was updated to `v1.11.1`
to please static analysers and fix warnings for CVE-2022-21698.

Improvements:
* Upgrade libgit2 and fix static builds
  [#311](https://github.com/fluxcd/image-automation-controller/pull/311)
* Add support for fuzzing tests using oss-fuzz-build.
  [#314](https://github.com/fluxcd/image-automation-controller/pull/314)
* Add support for multiple fuzz sanitizers
  [#317](https://github.com/fluxcd/image-automation-controller/pull/317)
* Update dependencies (fix CVE-2022-21698)
  [#319](https://github.com/fluxcd/image-automation-controller/pull/319)

## 0.20.0

**Release date:** 2022-02-01

This prerelease comes with support for referencing `GitRepositories` from another namespace
using the `spec.sourceRef.namespace` field in `ImageUpdateAutomations`.

Platform admins can disable cross-namespace references with the
`--no-cross-namespace-refs=true` flag. When this flag is set,
automations can only refer to Git repositories in the same namespace
as the automation object, preventing tenants from accessing another tenant's repositories.

The controller is now statically built and includes libgit2 along with
its main dependencies. The base image used to build and
run the controller, was changed from Debian Unstable (Sid) to Alpine 3.15.

The controller container images are signed with
[Cosign and GitHub OIDC](https://github.com/sigstore/cosign/blob/22007e56aee419ae361c9f021869a30e9ae7be03/KEYLESS.md),
and a Software Bill of Materials in [SPDX format](https://spdx.dev) has been published on the release page.

Starting with this version, the controller deployment conforms to the
Kubernetes [restricted pod security standard](https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted):
- all Linux capabilities were dropped
- the root filesystem was set to read-only
- the seccomp profile was set to the runtime default
- run as non-root was enabled
- the user and group ID was set to 65534

**Breaking changes**:
- The use of new seccomp API requires Kubernetes 1.19.
- The controller container is now executed under 65534:65534 (userid:groupid).
  This change may break deployments that hard-coded the user ID of 'controller' in their PodSecurityPolicy.

Features:
- Add support for cross-namespace sourceRef in ImageUpdateAutomation
  [#299](https://github.com/fluxcd/image-automation-controller/pull/299)
- Allow disabling cross-namespace references
  [#305](https://github.com/fluxcd/image-automation-controller/pull/305)

Improvements:
- Publish SBOM and sign release artifacts
  [#302](https://github.com/fluxcd/image-automation-controller/pull/302)
- Drop capabilities, enable seccomp and enforce runAsNonRoot
  [#295](https://github.com/fluxcd/image-automation-controller/pull/295)
- Statically build using musl toolchain and target alpine
  [#303](https://github.com/fluxcd/image-automation-controller/pull/303)

## 0.19.0

**Release date:** 2022-01-07

This prerelease comes with an update to the Kubernetes and controller-runtime dependencies
to align them with the Kubernetes 1.23 release.

In addition, the controller is now built with Go 1.17.

Improvements:
* Update Go to v1.17
  [#248](https://github.com/fluxcd/image-automation-controller/pull/248)
* Log the error when removing the working dir fails
  [#287](https://github.com/fluxcd/image-automation-controller/pull/287)

Fixes:
* Fix potentially broken support for macOS
  [#278](https://github.com/fluxcd/image-automation-controller/pull/278)
* Move Path check into switch case
  [#284](https://github.com/fluxcd/image-automation-controller/pull/284)

## 0.18.0

**Release date:** 2021-11-23

This prerelease updates several dependencies to their latest version,
solving an issue with `rest_client_request_latency_seconds_.*` high
cardinality metrics.

Improvements:
* Update controller-runtime to v0.10.2
  [#268](https://github.com/fluxcd/image-automation-controller/pull/268)
* Update image-reflector-controller and source-controller
  [#269](https://github.com/fluxcd/image-automation-controller/pull/269)

Fixes:
* Remove deprecated io/ioutil
  [#267](https://github.com/fluxcd/image-automation-controller/pull/267)

## 0.17.1

**Release date:** 2021-11-11

This prerelease comes with a bug fix to the image setter, ensuring it does not
accidentally replace a valid image reference with an invalid one.

Fixes:
* Replace strings.TrimRight with strings.TrimSuffix
  [#262](https://github.com/fluxcd/image-automation-controller/pull/262)

## 0.17.0

**Release date:** 2021-11-09

This prerelease comes with improvements to alerting.
The controller no longer emits events when no updates were made to the upstream Git repository.
After a successful Git push, the controller emits an event that contains the commit message.

Improvements:
* Add the commit message to the event body
  [#259](https://github.com/fluxcd/image-automation-controller/pull/259)

Fixes:
* Fix unhandled error in signing key retrieval
  [#258](https://github.com/fluxcd/image-automation-controller/pull/258)
* Use strings.TrimRight to determine image name
  [#257](https://github.com/fluxcd/image-automation-controller/pull/257)

## 0.16.1

**Release date:** 2021-11-04

This prerelease adds more improvements around the `libgit2` C library
used for Git transport operations, ensuring they respect the operation
`timeout` specified in `GitRepositorySpec` of the respective `GitRepository`
referenced in `GitCheckoutSpec`.

Improvements:
* Respect PKG_CONFIG_PATH from the environment
  [#251](https://github.com/fluxcd/image-automation-controller/pull/251)
* Pass context to libgit2.RemoteCallbacks
  [#252](https://github.com/fluxcd/image-automation-controller/pull/252)

## 0.16.0

**Release date:** 2021-10-28

This prerelease finalizes the improvements around the `libgit2` C library.
Users who noticed a memory increase after upgrading to `v0.15.0` are highly
encouraged to update to this new version as soon as possible, as this release
stabilizes it again.

In addition, [support for sprig functions](https://github.com/fluxcd/image-automation-controller/blob/v0.16.0/docs/spec/v1beta1/imageupdateautomations.md#commit-message-with-template-functions)
has been added in this release; allowing for more complex manipulations and
transformations of the commit message.

Improvements:
* Add support for the sprig functions library
  [#223](https://github.com/fluxcd/image-automation-controller/pull/223)
* controllers: use new `git` contract
  [#239](https://github.com/fluxcd/image-automation-controller/pull/239)

## 0.15.0

**Release date:** 2021-10-08

This prerelease improves the configuration of the `libgit2` C library, solving
most issues around private key formats (e.g. PKCS#8 and ED25519) by ensuring
it is linked against OpenSSL and LibSSH2.

Improvements:
* Update github.com/libgit2/git2go to v31.6.1
  [#222](https://github.com/fluxcd/image-automation-controller/pull/222)
* Use pkg/runtime consts for log levels
  [#232](https://github.com/fluxcd/image-automation-controller/pull/232)
* Update fluxcd/image-reflector-controller to v0.12.0
  [#233](https://github.com/fluxcd/image-automation-controller/pull/233)

Fixes:
* Provide a sample of v1beta1 ImageUpdateAutomation
  [#219](https://github.com/fluxcd/image-automation-controller/pull/219)
* Fix nil-dereference in controller
  [#224](https://github.com/fluxcd/image-automation-controller/pull/224)

## 0.14.1

**Release date:** 2021-08-05

This prerelease comes with an update to the Kubernetes and controller-runtime
dependencies to align them with the Kubernetes 1.21.3 release.

Improvements:
* Update dependencies
  [#211](https://github.com/fluxcd/image-automation-controller/pull/211)
* Fail push if a ref update is rejected
  [#195](https://github.com/fluxcd/image-automation-controller/pull/195)

## 0.14.0

**Release date:** 2021-06-28

This prerelease promotes the API version from `v1alpha2` to `v1beta1`.

:warning: If you have migrated your YAML files from `v1alpha1` to
`v1alpha2`, no action is necessary at present since Kubernetes will
automatically convert between `v1alpha2` and `v1beta1`.

You may wish to migrate YAML files to `v1beta1` to prepare for
`v1alpha2` being deprecated eventually. This is simply a case of
replacing the `apiVersion` field value:

    apiVersion: image.toolkit.fluxcd.io/v1beta1

Instructions for migrating from _`v1alpha1`_ are included in the [API guide].

[API guide]: https://fluxcd.io/flux/components/image/imageupdateautomations/#migrating-from-v1alpha1

Improvements:
* Let people set the number of controller workers with a flag
  [#192](https://github.com/fluxcd/image-automation-controller/pull/192)
* Output trace information to the debug log
  [#190](https://github.com/fluxcd/image-automation-controller/pull/190)

## 0.13.0

**Release date:** 2021-06-22

This prerelease comes with changes to the base image used to build the controller,
replacing Alpine with Debian slim.

Improvements:
* Use Debian as base image
  [#187](https://github.com/fluxcd/image-automation-controller/pull/187)

## 0.12.0

**Release date:** 2021-06-10

This prerelease comes with an update to the Kubernetes and controller-runtime
dependencies to align them with the Kubernetes 1.21 release, and an update to
the YAML packages (used by the controller to patch Kubernetes manifests) to
fix long-standing issues like panics on YAML with non-ASCII characters.

This update to `gopkg.in/yaml.v3` means that the indentation style changed:

From:

```yaml
spec:
  containers:
  - name: one
    image: image1:v1.0.0 # {"$imagepolicy": "automation-ns:policy1"}
  - name: two
    image: image2:v1.0.0 # {"$imagepolicy": "automation-ns:policy2"}
```

To:

```yaml
spec:
  containers:
    - name: one
      image: image1:v1.0.0 # {"$imagepolicy": "automation-ns:policy1"}
    - name: two
      image: image2:v1.0.0 # {"$imagepolicy": "automation-ns:policy2"}
```

Improvements:
* Update go-yaml with changes to indentation style
  [#182](https://github.com/fluxcd/image-automation-controller/pull/182)
* Add nightly builds workflow and allow RC releases
  [#184](https://github.com/fluxcd/image-automation-controller/pull/184)
* Update dependencies
  [#183](https://github.com/fluxcd/image-automation-controller/pull/183)

## 0.11.0

**Release date:** 2021-06-02

This prerelease replaces `go-git` usage with `libgit2` for clone, fetch and push operations.

The `gitImplementation` field in the referenced `GitRepository` is ignored. The
automation controller cannot use shallow clones or submodules, so there is no
reason to use the `go-git` implementation rather than `libgit2`.

Improvements:
* Use libgit2 for clone, fetch, push
  [#177](https://github.com/fluxcd/image-automation-controller/pull/177)
  
## 0.10.1

**Release date:** 2021-06-02

This prerelease comes with an update to the `go-git` implementation
dependency, bumping the version to `v5.4.2`. This should resolve any
issues with `object not found` and `empty git-upload-pack given`
errors that were thrown for some Git repositories since `0.10.0`.

Fixes:
* Update go-git to v5.4.2
  [#174](https://github.com/fluxcd/image-automation-controller/pull/174)

## 0.10.0

**Release date:** 2021-05-26

This prerelease updates the source-controller dependencies to `v0.13.0`
to include changes to Git and OpenPGP related dependencies.

Improvements:
* Update source-controller to v0.13.0
  [#169](https://github.com/fluxcd/image-automation-controller/pull/169)
* Switch to `github.com/ProtonMail/go-crypto/openpgp`
  [#169](https://github.com/fluxcd/image-automation-controller/pull/169)
* Update source-controller/api to v0.13.0
  [#170](https://github.com/fluxcd/image-automation-controller/pull/170)

## 0.9.1

**Release date:** 2021-05-06

This prerelease comes with a fix to the image name setter.

Fixes:
* Fix image name marker
  [#162](https://github.com/fluxcd/image-automation-controller/pull/162)
* spec: fix formatting `v1alpha1` -> `v1alpha2` table
  [#156](https://github.com/fluxcd/image-automation-controller/pull/156)

## 0.9.0

**Release date:** 2021-04-22

This prerelease deprecates the `v1alpha1` API declarations in favor
of the new `v1alpha2` API. The steps required to rewrite your
`v1alpha1` objects to `v1alpha2` [have been documented in the `v1alpha2`
spec](https://github.com/fluxcd/image-automation-controller/blob/api/v0.9.0/docs/spec/v1alpha2/imageupdateautomations.md#example-of-rewriting-a-v1alpha1-object-to-v1alpha2).

Improvements:
* Add v1alpha2 API version
  [#139](https://github.com/fluxcd/image-automation-controller/pull/139)
* Move to ImagePolicy v1alpha2
  [#153](https://github.com/fluxcd/image-automation-controller/pull/153)
* Update source-controller/api to v0.12.0
  [#154](https://github.com/fluxcd/image-automation-controller/pull/154)

## 0.8.0

**Release date:** 2021-04-06

This prerelease adds support for signing commits with GPG.

This prerelease comes with a breaking change to the leader election ID
from `e189b2df.fluxcd.io` to `image-reflector-controller-leader-election`
to be more descriptive. This change should not have an impact on most
installations, as the default replica count is `1`. If you are running
a setup with multiple replicas, it is however advised to scale down
before upgrading.

The controller exposes a gauge metric to track the suspended status
of `ImageUpdateAutomation` objects: `gotk_suspend_status{kind,name,namespace}`.

Features:
* Enable GPG Signing of Commits
  [#136](https://github.com/fluxcd/image-automation-controller/pull/136)

Improvements:
* Record suspension metrics
  [#129](https://github.com/fluxcd/image-automation-controller/pull/129)
* Update ImageUpdateAutomation Status with Patch
  [#132](https://github.com/fluxcd/image-automation-controller/pull/132)
* Set leader election deadline to 30s
  [#137](https://github.com/fluxcd/image-automation-controller/pull/137)
* Update kyaml to v0.10.16
  [#141](https://github.com/fluxcd/image-automation-controller/pull/141)

Fixes:
* Ensure that an unchanged image is not in update result
  [#144](https://github.com/fluxcd/image-automation-controller/pull/144)
* Fix problem with pushing further commits to a "push branch"
  [#143](https://github.com/fluxcd/image-automation-controller/pull/143)
* Ignore broken symlinks and outside path, in commit
  [#142](https://github.com/fluxcd/image-automation-controller/pull/142)

## 0.7.0

**Release date:** 2021-03-17

This prerelease comes with support for restricting the
image updates to a path relative to the Git repository root
with `.spec.update.path`.

The controller can push changes to a different branch
than the one used for cloning when configured with `.spec.push.branch`.

The commit message template supports listing the 
images that were updated along with the resource kind and name e.g.:

```yaml
spec:
  commit:
    messageTemplate: |
      Automated image update
      
      Automation name: {{ .AutomationObject }}
      
      Files:
      {{ range $filename, $_ := .Updated.Files -}}
      - {{ $filename }}
      {{ end -}}
      
      Objects:
      {{ range $resource, $_ := .Updated.Objects -}}
      - {{ $resource.Kind }} {{ $resource.Name }}
      {{ end -}}
      
      Images:
      {{ range .Updated.Images -}}
      - {{.}}
      {{ end -}}
```

Features:
* Allow specifying the path for manifests updates
  [#126](https://github.com/fluxcd/image-automation-controller/pull/126)
* Push to branch
  [#121](https://github.com/fluxcd/image-automation-controller/pull/121)
* Supply update result value to the commit message template
  [#119](https://github.com/fluxcd/image-automation-controller/pull/119)

Improvements:
* Update runtime dependencies
  [#124](https://github.com/fluxcd/image-automation-controller/pull/124)

## 0.6.1

**Release date:** 2021-02-25

This prerelease improves the formatting of error messages returned when
a `git push` to an upstream Git repository fails.

Improvements:
* Better error messages from `git push`
  [#115](https://github.com/fluxcd/image-automation-controller/pull/115)

## 0.6.0

**Release date:** 2021-02-24

This prerelease comes with various updates to the controller's
dependencies.

The Kubernetes custom resource definitions are packaged as
a multi-doc YAML asset and published on the GitHub release page.

Improvements:
* Refactor release workflow
  [#112](https://github.com/fluxcd/image-automation-controller/pull/112)
* Update dependencies
  [#111](https://github.com/fluxcd/image-automation-controller/pull/111)

## 0.5.0

**Release date:** 2021-02-12

Alpine has been updated to `3.13`, making it possible to move away from `edge`
for `libgit2` and `musl` dependencies.

`pprof` endpoints have been enabled on the metrics server, making it easier to
collect runtime information to for example debug performance issues.

A bug has been fixed that caused SSH authentication for the `libgit2` Git
implementation to fail.

Improvements:
* Enable pprof endpoints on metrics server
  [#104](https://github.com/fluxcd/image-automation-controller/pull/104)
* Update Alpine to v3.13
  [#105](https://github.com/fluxcd/image-automation-controller/pull/105)
* Use musl and libgit2 packages from v3.13 branch
  [#107](https://github.com/fluxcd/image-automation-controller/pull/107)
* Update kyaml to v0.10.9
  [#108](https://github.com/fluxcd/image-automation-controller/pull/108)

Fixes:
* Test git over SSH too, and correct hard-wired implementation causing
  SSH/libgit2 problems
  [#109](https://github.com/fluxcd/image-automation-controller/pull/109)

## 0.4.0

**Release date:** 2021-01-22

This prerelease reforms the update strategy types API in a backwards
compatible manner.

In addition, it comes with two new argument flags introduced to support
configuring the QPS (`--kube-api-qps`) and burst (`--kube-api-burst`)
while communicating with the Kubernetes API server.

The `LocalObjectReference` from the Kubernetes core has been replaced
with our own, making the `name` a required field. The impact of this
should be limited to direct API consumers only, as the field was
already required by controller logic.

Improvements:
* Update fluxcd/pkg/runtime to v0.8.0
  [#98](https://github.com/fluxcd/image-automation-controller/pull/98)
* Reform update strategy types
  [#95](https://github.com/fluxcd/kustomize-controller/pull/95)
* Add API reference for ImageUpdateAutomation
  [#94](https://github.com/fluxcd/image-automation-controller/pull/94)  

## 0.3.1

**Release date:** 2021-01-18

This prerelease comes with updates to Kubernetes and Kustomize dependencies.
The Kubernetes packages were updated to v1.20.2 and kyaml to v0.10.6.

## 0.3.0

**Release date:** 2021-01-15

This prerelease adds support for the `libgit2` implementation,
making it possible to push changes to Git servers that require
support for the v2 protocol, like Azure DevOps.

The container image for ARMv7 and ARM64 that used to be published
separately as `image-automation-controller:*-arm64` has been merged
with the AMD64 image.

Improvements:
* Update to kyaml 0.10.5
  [#87](https://github.com/fluxcd/kustomize-controller/pull/87)
* Upgrade controller-runtime to v0.7.0
  [#84](https://github.com/fluxcd/kustomize-controller/pull/84)
* Libgit2 support
  [#82](https://github.com/fluxcd/kustomize-controller/pull/82)
* Publish as single multi-arch Docker image
  [#80](https://github.com/fluxcd/kustomize-controller/pull/80)

## 0.2.0

**Release date:** 2021-01-06

This prerelease comes with a fix to the manifest update
mechanism. The controller now only writes files that were
actually updated by an image policy setter instead of 
reformatting all Kubernetes YAMLs.

Starting with this version, the `spec.checkout.branch`
field is mandatory.

Fixes:
* Screen files, and output only those updated
  [#73](https://github.com/fluxcd/kustomize-controller/pull/73)

Improvements:
* Record last pushed commit in status
  [#75](https://github.com/fluxcd/kustomize-controller/pull/75)
* Make the branch field mandatory
  [#74](https://github.com/fluxcd/kustomize-controller/pull/74)

## 0.1.0

**Release date:** 2020-12-10

This is the first _prerelease_ of image-automation-controller and its
API. The controller watches ImagePolicy objects (as in the
[image-reflector-controller API][imagev1]) and commits updates to
files in git when the "latest image" changes.

The controller and API conform to the GitOps Toolkit conventions, and
will work with the `flux` CLI, and dashboards using the standard
metrics, and so on.

This release supports:

 - updating YAML files with image refs, names or tags according to
   [markers you add][marker-example] in the YAML.
 - supplying a custom commit message

[marker-example]: https://github.com/fluxcd/image-automation-controller#adding-a-marker-to-the-yaml-to-update
