# Image automation controller

This is part of the image update automation, as outlined in

 - [this post](https://squaremo.dev/posts/gitops-controllers/); and refined in
 - [this design](https://github.com/squaremo/image-reflector-controller/pull/5)

Its sibling repository
[image-reflector-controller](https://github.com/squaremo/image-reflector-controller)
implements the image metadata reflection controller (scans container
image repositories and reflects the metadata in Kubernetes resources);
this repository implements the image update automation controller.
