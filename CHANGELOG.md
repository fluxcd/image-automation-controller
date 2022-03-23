# Changelog

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

[API guide]: https://fluxcd.io/docs/components/image/imageupdateautomations/#migrating-from-v1alpha1

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
