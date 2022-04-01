# Image automation controller

[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/4789/badge)](https://bestpractices.coreinfrastructure.org/projects/4789)
[![report](https://goreportcard.com/badge/github.com/fluxcd/image-automation-controller)](https://goreportcard.com/report/github.com/fluxcd/image-automation-controller)
[![license](https://img.shields.io/github/license/fluxcd/image-automation-controller.svg)](https://github.com/fluxcd/image-automation-controller/blob/main/LICENSE)
[![release](https://img.shields.io/github/release/fluxcd/image-automation-controller/all.svg)](https://github.com/fluxcd/image-automation-controller/releases)

This controller automates updates to YAML when new container images
are available.

Its sibling,
[image-reflector-controller](https://github.com/fluxcd/image-reflector-controller),
scans container image repositories and reflects the metadata in
Kubernetes resources. This controller reacts to that image metadata by
updating YAML files in a git repository, and committing the changes.

## How to install it

Please see the [installation and use
guide](https://fluxcd.io/docs/guides/image-update/).

## How to work on it

The shared library `libgit2` needs to be installed to test or build
locally. The version required corresponds to the version of git2go
(which are Go bindings for libgit2), according to [this
table](https://github.com/libgit2/git2go#which-go-version-to-use).

See
https://github.com/fluxcd/source-controller/blob/main/DEVELOPMENT.md#installing-required-dependencies
for instructions on how to install `libgit2`.
