# Changelog

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
