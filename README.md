# Image automation controller

This is part of the image update automation, as outlined in

 - [this post](https://squaremo.dev/posts/gitops-controllers/); and refined in
 - [this design](https://github.com/fluxcd/image-reflector-controller/pull/5)

Its sibling repository
[image-reflector-controller](https://github.com/fluxcd/image-reflector-controller)
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

    kubectl apply -f https://raw.githubusercontent.com/fluxcd/source-controller/v0.2.2/config/crd/bases/source.toolkit.fluxcd.io_gitrepositories.yaml

**To install the image reflector controller**

This controller relies on the image reflector controller. A working
configuration for the latter can be applied straight from the GitHub
repository:

    kustomize build https://github.com/fluxcd/image-reflector-controller/config/default/?ref=main | kubectl apply -f-

### Installing the automation controller

You can apply a working configuration directly from GitHub:

    kustomize build https://github.com/fluxcd/image-automation-controller/config/default?ref=main | kubectl apply -f-

or, in a clone of this repository,

    make docker-build deploy

You will need to do the latter if you're trying the controller on a
branch other than `main`.

## How to use it

Here is a quick example of configuring an automation. I'm going to use
[cuttlefacts-app][cuttlefacts-app-repo] because it's minimal and easy
to follow.

### Image policy

[The deployment][cuttlefacts-app-deployment] in cuttlefacts-app uses
the image `cuttlefacts/cuttlefacts-app`. We'll automate that so it
gets updated when there's a semver-tagged image, e.g.,
`cuttlefacts/cuttlefacts-app:v1.0.0`.

Keeping track of the most recent image takes two resources: an
`ImageRepository`, to scan DockerHub for the image's tags, and an
`ImagePolicy`, to give the particular policy for selecting an image
(here, a semver range).

The `ImageRepository`:

```bash
$ cat > image.yaml <<EOF
apiVersion: image.toolkit.fluxcd.io/v1alpha1
kind: ImageRepository
metadata:
  name: app-image
spec:
  image: cuttlefacts/cuttlefacts-app
EOF
```

... and the policy:

```bash
$ cat > policy.yaml <<EOF
apiVersion: image.toolkit.fluxcd.io/v1alpha1
kind: ImagePolicy
metadata:
  name: app-policy
spec:
  imageRepositoryRef:
    name: app-image
  policy:
    semver:
      range: 1.0.x
EOF
```

Apply these into the cluster, and the image reflector controller
(installed as a prerequisite, above) will scan for the tags of the
image and figure out which one to use. You can see this by asking for
the status of the image policy:

```bash
$ kubectl get imagepolicy app-policy
NAME         LATESTIMAGE
app-policy   cuttlefacts/cuttlefacts-app:1.0.0
```

### Creating the git repository object

You need a writable git repository, so fork
[`cuttlefacts-app`][cuttlefacts-app-repo] to your own account, and
copy the SSH URL. For me that's
`ssh://git@github.com/squaremo/cuttlefacts-app` (when you see that git
URL, substitute your own fork -- it's the "squaremo" that will
differ).

First, I'll set up a `GitRepository` giving access to the git
repo. For read/write access, I need a deploy key (or some other means
of authenticating, but a deploy key will be easiest). To make a key
(give an empty passphrase):

    ssh-keygen -f identity

You also need the host keys from github. To get the host keys and
verify them:

    ssh-keyscan github.com > known_hosts
    ssh-keygen -l -f known_hosts

Check that the fingerprint matches one [published by
GitHub][github-fingerprints].

Now you can make a secret with the deploy key and known_hosts file:

    kubectl create secret generic cuttlefacts-deploy --from-file=identity --from-file=known_hosts

Those two filenames -- `identity` and `known_hosts` -- are what the
source controller library code expects, which makes it easier for the
automation controller to use the `GitRepository` type.

You also need to install the deploy key in GitHub. Copy it from
`identity.pub` (that's the _public_ part of the key):

```bash
$ cat identity.pub
ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDKM2wTSz5VyL2UCLh3ke9XUO1WUmAf
[...]w2FFnV24AGhWdP5lPOS/Jv64+OfMSF5E/e4dwVs= mikeb@laptop.lan
```

... and add under `Settings / Deploy keys` for your fork on GitHub,
giving it write access.

Now you can create a `GitRepository` which will provide access to the
git repository within the cluster. Remember to change the URL; it's
probably easiest, if you're copying & pasting, to run the following
then edit `repo.yaml` afterwards.

```bash
$ cat > repo.yaml <<EOF
apiVersion: source.toolkit.fluxcd.io/v1beta1
kind: GitRepository
metadata:
  name: cuttlefacts-repo
spec:
  url: ssh://git@github.com/squaremo/cuttlefacts-app
  interval: 1m
  secretRef:
    name: cuttlefacts-deploy
EOF
$ $EDITOR repo.yaml
```

Create the repository object; be aware that unless you're running the
full GitOps toolkit suite, there will be no controller acting on it
(and doesn't need to be, for the purpose of this run-through).

```bash
$ kubectl apply -f repo.yaml
gitrepository.source.toolkit.fluxcd.io/cuttlefacts-repo created
$ kubectl get gitrepository
NAME               URL                                             READY   STATUS   AGE
cuttlefacts-repo   ssh://git@github.com/squaremo/cuttlefacts-app                    9s
```

### Adding a marker to the YAML to update

To tell the controller what to update, you add some markers to the
files to be updated. Each marker says which field to update, and which
image policy to use for the new value.

In this case, it's the image in the deployment that needs to be
updated, with the latest image from the image policy made
earlier. Edit the file either locally or through GitHub, and add a
marker to the file `deploy/deployment.yaml` at the line with the image
field, `image: cuttlefacts/cuttlefacts-app`. The surrounding lines
look like this:

```
      containers:
      - name: server
        image: cuttlefacts/cuttlefacts-app
        imagePullPolicy: IfNotPresent
```

With the marker, they look like this:

```
      containers:
      - name: server
        image: cuttlefacts/cuttlefacts-app # {"$imagepolicy": "default:app-policy"}
        imagePullPolicy: IfNotPresent
```

The marker is a comment at the end of the `image:` line, with a JSON
value (so remember the double quotes), naming the image policy object
to use for the value. A `:` character separates the namespace from the
name of the `ImagePolicy` object. (The namespace is default because it
wasn't specified in the manifest (`policy.yaml`) or when it was
applied.)

Commit that change, and push it if you made the commit locally.

### Creating the automation object

Now we have an image policy, which calculates the most recent image,
and a git repository to update, and we've marked the field to update,
in a file. The last ingredient is to tie these together with an
`ImageUpdateAutomation` resource:

```
$ cat > update.yaml <<EOF
apiVersion: image.toolkit.fluxcd.io/v1alpha1
kind: ImageUpdateAutomation
metadata:
  name: update-app
spec:
  checkout:
    gitRepositoryRef:
      name: cuttlefacts-repo
  update:
    setters:
      paths:
      - .
  commit:
    authorName: UpdateBot
    authorEmail: bot@example.com
EOF
```

The git repository object is mentioned, and the `setters` value gives
the paths to apply updates under.

Apply the file to create the automation object:

    kubectl apply -f update.yaml

Once that's created, it should quickly commit a change to the git
repository, to make the image in the deployment match the most recent
given by the image policy. Here's an example, [from my own
repository][squaremo-auto-commit].

[source-controller]: https://github.com/fluxcd/source-controller
[image-reflector]: https://github.com/fluxcd/image-reflector-controller
[gotk]: https://toolkit.fluxcd.io
[cuttlefacts-app-repo]: https://github.com/cuttlefacts/cuttlefacts-app
[github-fingerprints]: https://docs.github.com/en/github/authenticating-to-github/githubs-ssh-key-fingerprints
[cuttlefacts-app-deployment]: https://github.com/cuttlefacts/cuttlefacts-app/blob/master/deploy/deployment.yaml
[squaremo-auto-commit]: https://github.com/squaremo/cuttlefacts-app-auto-setters/commit/edb6e7c0724bcc2226dc3077558f747e7adfb8e8
