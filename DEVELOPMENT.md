# Development

> **Note:** Please take a look at <https://fluxcd.io/docs/contributing/flux/>
> to find out about how to contribute to Flux and how to interact with the
> Flux Development team.

## Installing required dependencies

The dependency [libgit2](https://libgit2.org/) needs to be installed to be able
to run source-controller or its test-suite locally (not in a container).

In case this dependency is not present on your system (at the expected
version), the first invocation of a `make` target that requires the
dependency will attempt to compile it locally to `hack/libgit2`. For this build
to succeed; CMake, OpenSSL 1.1 and LibSSH2 must be present on the system.

Triggering a manual build of the dependency is possible as well by running
`make libgit2`. To enforce the build, for example if your system dependencies
match but are not linked in a compatible way, append `LIBGIT2_FORCE=1` to the
`make` command.

### macOS

```console
$ # Ensure libgit2 dependencies are available
$ brew install cmake openssl@1.1 libssh2 pkg-config
$ LIBGIT2_FORCE=1 make libgit2
```

#### openssl and pkg-config

You may see this message when trying to run a build:

```
# pkg-config --cflags  -- libgit2
Package openssl was not found in the pkg-config search path.
Perhaps you should add the directory containing `openssl.pc'
to the PKG_CONFIG_PATH environment variable
Package 'openssl', required by 'libgit2', not found
pkg-config: exit status 1
```

On some macOS systems `brew` will not link the pkg-config file for
openssl into expected directory, because it clashes with a
system-provided openssl. When installing it, or if you do

```console
brew link openssl@1.1
Warning: Refusing to link macOS provided/shadowed software: openssl@1.1
If you need to have openssl@1.1 first in your PATH, run:
  echo 'export PATH="/usr/local/opt/openssl@1.1/bin:$PATH"' >> ~/.profile

For compilers to find openssl@1.1 you may need to set:
  export LDFLAGS="-L/usr/local/opt/openssl@1.1/lib"
  export CPPFLAGS="-I/usr/local/opt/openssl@1.1/include"

For pkg-config to find openssl@1.1 you may need to set:
  export PKG_CONFIG_PATH="/usr/local/opt/openssl@1.1/lib/pkgconfig"
```

.. it tells you (in the last part of the message) how to add `openssl`
to your `PKG_CONFIG_PATH` so it can be found.

### Linux

```console
$ # Ensure libgit2 dependencies are available
$ pacman -S cmake openssl libssh2
$ LIBGIT2_FORCE=1 make libgit2
```

**Note:** Example shown is for Arch Linux, but likewise procedure can be
followed using any other package manager, e.g. `apt`.

## How to run the test suite

Prerequisites:
* go >= 1.17
* kubebuilder >= 2.3
* kustomize >= 3.1

You can run the unit tests by simply doing

```bash
make test
```
