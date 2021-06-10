# Changelog

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
