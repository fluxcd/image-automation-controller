# Changelog

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
