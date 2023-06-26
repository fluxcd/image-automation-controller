# Development

> **Note:** Please take a look at <https://fluxcd.io/contributing/flux/>
> to find out about how to contribute to Flux and how to interact with the
> Flux Development team.

## Installing required dependencies

There are a number of dependencies required to be able to run the controller and its test suite locally:

- [Install Go](https://golang.org/doc/install)
- [Install Kustomize](https://kubernetes-sigs.github.io/kustomize/installation/)
- [Install Docker](https://docs.docker.com/engine/install/)
- (Optional) [Install Kubebuilder](https://book.kubebuilder.io/quick-start.html#installation)

The following dependencies are also used by some of the `make` targets:

- `controller-gen` (v0.7.0)
- `gen-crd-api-reference-docs` (v0.3.0)
- `setup-envtest` (latest)

If any of the above dependencies are not present on your system, the first invocation of a `make` target that requires them will install them.

## How to run the test suite

Prerequisites:
* Go >= 1.20

You can run the test suite by simply doing

```sh
make test
```
## How to run the controller locally

Install the controller's CRDs on your test cluster:

```sh
make install
```

Note that `image-automation-controller` depends on [source-controller](https://github.com/fluxcd/source-controller) to acquire its artifacts and [image-reflector-controller](https://github.com/fluxcd/image-reflector-controller) to access container image metadata. Ensure that they are both running on your test cluster prior to running the `image-automation-controller`.

Run the controller locally:

```sh
make run
```

## How to install the controller

### Building the container image

Set the name of the container image to be created from the source code. This will be used when building, pushing and referring to the image on YAML files:

```sh
export IMG=registry-path/kustomize-controller
export TAG=latest
```
Build and push the container image, tagging it as `$(IMG):$(TAG)`:

```sh
BUILD_ARGS=--push make docker-build
```
**Note**: `make docker-build` will build images for the `amd64`,`arm64` and `arm/v7` architectures.

If you get the following error when building the docker container:
```
Multiple platforms feature is currently not supported for docker driver.
Please switch to a different driver (eg. "docker buildx create --use")
```

you may need to create and switch to a new builder that supports multiple platforms:

```sh
docker buildx create --use
```

### Deploying into a cluster

Deploy `image-automation-controller` into the cluster that is configured in the local kubeconfig file (i.e. `~/.kube/config`):

```sh
make deploy
```

### Debugging controller with VSCode

Create a `.vscode/launch.json` file:
```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch Package",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/main.go"
        }
    ]
}
```

Start debugging by either clicking `Run` > `Start Debugging` or using
the relevant shortcut.
