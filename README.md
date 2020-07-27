# Image automation controller

This is part of the image update automation, as outlined in

 - [this post](https://squaremo.dev/posts/gitops-controllers/); and refined in
 - [this design](https://github.com/squaremo/image-reflector-controller/pull/5)

Its sibling repository
[image-reflector-controller](https://github.com/squaremo/image-reflector-controller)
implements the image metadata reflection controller (scans container
image repositories and reflects the metadata in Kubernetes resources);
this repository implements the image update automation controller.

## How to install it

### Prerequisites

At present this works with GitRepository custom resources as defined
in the [`source-controller`][source-controller] types; and, the
[`image-reflector-controller`][image-reflector]. GitRepository
resources are used to describe how to access the git repository to
update. The image reflector scans container image metadata, and
reflects it into the cluster as resources which this controller uses
as input to make updates; for example, by changing deployments so they
use the most recent version of an image.

**To install the GitRepository CRD**

This controller only needs the custom resource definition (CRD) for
the GitRepository kind, and doesn't need the source-controller itself.

If you're not already using the [GitOps toolkit][gotk], you can just
install the custom resource definition for GitRepository:

    kubectl apply -f https://raw.githubusercontent.com/fluxcd/source-controller/master/config/crd/bases/source.fluxcd.io_gitrepositories.yaml

**To install the image reflector controller**

This controller relies on the image reflector controller. A working
configuration for the latter can be applied straight from the GitHub
repository (NB `-k`):

    kubectl apply -k github.com/squaremo/image-reflector-controller/config/default

### Installing the automation controller

You can apply a working configuration directly from GitHub:

    kubectl apply -k github.com/squaremo/image-automation-controller/config/default

or, in a clone of this repository,

    make docker-build deploy

## How to use it

 TODO

[source-controller]: https://github.com/fluxcd/source-controller
[image-reflector]: https://github.com/squaremo/image-reflector-controller
[gotk]: https://toolkit.fluxcd.io
