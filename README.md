# Image automation controller

This controller automates updates to YAML when new container images
are available.

Its sibling,
[image-reflector-controller](https://github.com/fluxcd/image-reflector-controller),
scans container image repositories and reflects the metadata in
Kubernetes resources. This controller reacts to that image metadata by
updating YAML files in a git repository, and committing the changes.

## How to install it

Please see the [installation and use
guide](https://toolkit.fluxcd.io/guides/image-update/).
