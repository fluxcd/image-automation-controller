# Changelog

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
