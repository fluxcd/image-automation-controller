# Image Update Automations

The `ImageUpdateAutomation` API defines an automation process that will update a
Git repository, based on `ImagePolicy` objects in the same namespace.

The updates are governed by marking fields to be updated in each YAML file. For
each field marked, the automation process checks the image policy named, and
updates the field value if there is a new image selected by the policy. The
marker format is shown in the [image automation guide][image-auto-guide].

## Example

The following is an example of keeping the images in a Git repository
up-to-date:

```yaml
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: podinfo
  namespace: default
spec:
  interval: 5m0s
  url: https://github.com/fluxcd/example
  ref:
    branch: main
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageRepository
metadata:
  name: podinfo
  namespace: default
spec:
  image: ghcr.io/stefanprodan/podinfo
  interval: 5h
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImagePolicy
metadata:
  name: podinfo-policy
  namespace: default
spec:
  imageRepositoryRef:
    name: podinfo
  policy:
    semver:
      range: 5.0.x
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: podinfo-update
  namespace: default
spec:
  interval: 30m
  sourceRef:
    kind: GitRepository
    name: podinfo
  git:
    commit:
      author:
        email: fluxcdbot@users.noreply.github.com
        name: fluxcdbot
    push:
      branch: main
  update:
    path: ./
```

In the above example:

- A GitRepository named `podinfo` is created, indicated by the
  `GitRepository.metadata.name` field. The Git repository at
  `https://github.com/fluxcd/example` is assumed to contain YAML files with
  image policy markers, as described in [image automation
  guide][image-auto-guide], to update them.
- An ImageRepository named `podinfo` is created, indicated by the
  `ImageRepository.metadata.name` field. This scans all the tags for an image repository.
- An ImagePolicy named `podinfo-policy` is created, indicated by the
  `ImagePolicy.metadata.name` field.
- An ImageUpdateAutomation named `podinfo-update` is created, indicated by the
  `ImageUpdateAutomation.metadata.name` field.
- The ImagePolicy refers to the `podinfo` ImageRepository to query for all the
  tags related to an image, indicated by `ImagePolicy.spec.imageRepositoryRef`.
  These tags are then evaluated to select the latest image with tag based on the
  policy rules, indicated by `ImagePolicy.spec.policy`.
- The ImageUpdateAutomation refers to `podinfo` GitRepository as the source that
  should be kept up-to-date, indicated by
  `ImageUpdateAutomation.spec.sourceRef`.
- The image-automation-controller lists all the ImagePolicies in the
  ImageUpdateAutomation's namespace. It then checks out the Git repository
  `main` branch, as configured in `GitRepository.spec.ref.branch`. It then goes
  through the YAML manifests from the root of the Git repository, as configured
  in `ImageUpdateAutomation.spec.update.path` and applies updates based on the
  latest images from the image policies. The source changes are saved as a Git
  commit with the commit author defined in
  `ImageUpdateAutomation.spec.git.commit.author`. The commit is then push to the
  remote Git repository's `main` branch, indicated by
  `ImageUpdateAutomation.spec.git.push.branch`.
- The push commit hash is reported in the
  `ImageUpdateAutomation.status.lastPushCommit` field and the push time is
  reported in `.status.lastPushTime` field.

This example can be run by saving the manifest into
`imageupdateautomation.yaml`.

1. Apply the resource on the cluster:

```sh
kubectl apply -f imageupdateautomation.yaml
```

2. Run `kubectl get imageupdateautomation` to see the ImageUpdateAutomation:

```console
NAME             LAST RUN
podinfo-update   2024-03-17T22:22:34Z
```

3. Run `kubectl describe imageupdateautomation podinfo-update` to see the [Last
   Push Commit](#) and [Conditions](#conditions) in the ImageUpdateAutomation's
   Status:

```console
Status:
  Conditions:
    Last Transition Time:    2024-03-17T22:22:33Z
    Message:                 repository up-to-date
    Observed Generation:     1
    Reason:                  Succeeded
    Status:                  True
    Type:                    Ready
  Last Automation Run Time:  2024-03-17T22:22:34Z
  Last Push Commit:          3ebb95cc56d2db59bc6ffbe0d9dd0ea445edeb77
  Last Push Time:            2024-03-17T22:22:34Z
  Observed Generation:       1
  Observed Policies:
    Podinfo - Policy:
      Name:  ghcr.io/stefanprodan/podinfo
      Tag:   5.0.3
  Observed Source Revision:  main@sha1:3ebb95cc56d2db59bc6ffbe0d9dd0ea445edeb77
Events:
  Type    Reason     Age              From                         Message
  ----    ------     ----             ----                         -------
  Normal  Succeeded  5s (x2 over 6s)  image-automation-controller  repository up-to-date
  Normal  Succeeded  5s               image-automation-controller  pushed commit '3ebb95c' to branch 'main'
Update from image update automation
```

## Writing an ImageUpdateAutomation spec

As with all other Kubernetes config, an ImageUpdateAutomation needs
`apiVersion`, `kind`, and `metadata` fields. The name of an
ImageUpdateAutomation object must be a valid [DNS subdomain
name](https://kubernetes.io/docs/concepts/overview/working-with-objects/names#dns-subdomain-names).

An ImageUpdateAutomation also needs a [`.spec`
section](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status).

### Source reference

`.spec.sourceRef` is a required field to specify a reference to a source object
in the same namespace as the ImageUpdateAutomation or in another namespace. The
only supported source kind at the moment is `GitRepository`, which is used by
default if the `.spec.sourceRef.kind` is not specified. The source reference
name is a required field, `.spec.sourceRef.name`. The source reference namespace
is optional, `.spec.sourceRef.namespace`. If not specified, the source is
assumed to be in the same namespace as the ImageUpdateAutomation. The
GitRepository must contain the authentication configuration required to check
out the source, if any.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  sourceRef:
    name: <gitrepository-name>
    namespace: <gitrepository-namespace>
```

By default, GitRepository in a different namespace can be referenced. This can
be disabled by setting the controller flag `--no-cross-namespace-refs`.

The timeouts used in the Git operations for an ImageUpdateAutomation is derived
from the referenced GitRepository source. `GitRepository.spec.timeout` can be
tuned to adjust the Git operation timeout.

The proxy configurations are also derived from the referenced GitRepository
source. `GitRepository.spec.proxySecretRef` can be used to configure proxy use.

#### GitRepository Provider

`GitRepository` can be configured to specify an OIDC
[provider](https://fluxcd.io/flux/components/source/gitrepositories/#provider)
for authentication using `GitRepository.spec.provider` field. Image automation
controller can be configured to authenticate using the provider as described
below.

##### Azure

If the provider is set to `azure`, make sure the
[pre-requisites](https://fluxcd.io/flux/components/source/gitrepositories/#azure)
are satisfied. To configure image automation controller to use workload
identity,

- Create a managed identity to access Azure DevOps. Establish a federated
  identity credential between the managed identity and the
  image-automation-controller service account. In the default installation, the
  image-automation-controller service account is located in the `flux-system`
  namespace with name `image-automation-controller`. Ensure the federated
  credential uses the correct namespace and name of the
  image-automation-controller service account. For more details, please refer to
  this
  [guide](https://azure.github.io/azure-workload-identity/docs/quick-start.html#6-establish-federated-identity-credential-between-the-identity-and-the-service-account-issuer--subject).

- Add the managed identity to the Azure DevOps organization as a user. Ensure
  that the managed identity has the necessary permissions to access the Azure
  DevOps repository as described
  [here](https://learn.microsoft.com/en-us/azure/devops/integrate/get-started/authentication/service-principal-managed-identity?view=azure-devops#2-add-and-manage-service-principals-in-an-azure-devops-organization).

- Add the following patch to your bootstrap repository in
  flux-system/kustomization.yaml file.

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - gotk-components.yaml
  - gotk-sync.yaml
patches:
  - patch: |-
      apiVersion: v1
      kind: ServiceAccount
      metadata:
        name: image-automation-controller
        namespace: flux-system
        annotations:
          azure.workload.identity/client-id: <AZURE_CLIENT_ID>
        labels:
          azure.workload.identity/use: "true"
  - patch: |-
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: image-automation-controller
        namespace: flux-system
        labels:
          azure.workload.identity/use: "true"
      spec:
        template:
          metadata:
            labels:
              azure.workload.identity/use: "true"
```

### Git specification

`.spec.git` is a required field to specify Git configurations related to source
`checkout`, `commit` and `push` operations.

#### Checkout

`.spec.git.checkout` is an optional field to specify the Git reference to check
out. The `.spec.git.checkout.ref` field is the same as the
`GitRepository.spec.ref` field. It can be used to override the checkout
configuration in the referenced GitRepository. Not specifying this reference
defaults to the checkout reference of the associated GitRepository.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  git:
    checkout:
      ref:
        branch: <branch-name>
```

If `.spec.git.push` is unspecified, `.spec.git.checkout` will be used as the
push branch for any updates.

By default the controller will only do shallow clones, but this can be disabled
by starting the controller with flag `--feature-gates=GitShallowClone=false`.

#### Commit

`.spec.git.commit` is a required field to specify the details about the commit
made by the automation.

##### Author

`.spec.git.commit.author` is a required field to specify the commit author. The
author `.email` is required. The author `.name` is optional. The name and email
are used as the author of the commits made by the automation.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  git:
    commit:
      author:
        email: <author-email>
        name: <author-name>
``` 

##### Signing Key

`.spec.git.commit.signingKey` is an optional field to specify the signing PGP
key to sign the commits with. `.secretRef.name` refers to a Secret in the same
namespace as the ImageUpdateAutomation, containing an ASCII-armored PGP key, in
a field named `git.asc`. If the private key is protected by a passphrase, the
passphrase can be specified in the same Secret in a field named `passphrase`.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  git:
    commit:
      signingKey:
        secretRef:
          name: signing-key
...
---
apiVersion: v1
kind: Secret
metadata:
  name: signing-key
stringData:
  git.asc: |
    <ARMOR ENCODED PGP KEY>
  passphrase: <private-key-passphrase>
```

##### Message Template

`.spec.git.commit.messageTemplate` is an optional field to specify the commit
message template. If unspecified, a default commit message is used.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  git:
    commit:
      messageTemplate: |-
        Automated image update by Flux
```

**Deprecation Note:** The `Updated` template data available in v1beta1 API is
deprecated. `Changed` template data is recommended for template data, as it
accommodates for all the updates, including partial updates to just the image
name or the tag, not just full image with name and tag update. The old templates
will continue to work in v1beta2 API as `Updated` has not been removed yet. In
the next API version, `Updated` may be removed.

The message template also has access to the data related to the changes made by
the automation. The template is a [Go text template][go-text-template]. The data
available to the template have the following structure (not reproduced
verbatim):

```go
// TemplateData is the type of the value given to the commit message
// template.
type TemplateData struct {
	AutomationObject struct {
	  Name, Namespace string
	}
	Changed update.ResultV2
	Values map[string]string
}

// ResultV2 contains the file changes made during the update. It contains
// details about the exact changes made to the files and the objects in them. It
// has a nested structure file->objects->changes.
type ResultV2 struct {
	FileChanges map[string]ObjectChanges
}

// ObjectChanges contains all the changes made to objects.
type ObjectChanges map[ObjectIdentifier][]Change

// ObjectIdentifier holds the identifying data for a particular
// object. This won't always have a name (e.g., a kustomization.yaml).
type ObjectIdentifier struct {
	Name, Namespace, APIVersion, Kind string
}

// Change contains the setter that resulted in a Change, the old and the new
// value after the Change.
type Change struct {
	OldValue string
	NewValue string
	Setter   string
}
```

The `Changed` template data field also has a few helper methods to easily range
over the changed objects and changes:

```go
// Changes returns all the changes that were made in at least one update.
func (r ResultV2) Changes() []Change

// Objects returns ObjectChanges, regardless of which file they appear in.
func (r ResultV2) Objects() ObjectChanges
```

Example of using the methods in a template:

```yaml
spec:
  commit:
    messageTemplate: |
      Automated image update
      
      Automation name: {{ .AutomationObject }}

      Files:
      {{ range $filename, $_ := .Changed.FileChanges -}}
      - {{ $filename }}
      {{ end -}}

      Objects:
      {{ range $resource, $changes := .Changed.Objects -}}
      - {{ $resource.Kind }} {{ $resource.Name }}
        Changes:
      {{- range $_, $change := $changes }}
          - {{ $change.OldValue }} -> {{ $change.NewValue }}
      {{ end -}}
      {{ end -}}
```

With template functions, it is possible to manipulate and transform the supplied
data in order to generate more complex commit messages. 

```yaml
spec:
  commit:
    messageTemplate: |
      Automated image update
      
      Automation name: {{ .AutomationObject }}

      Files:
      {{ range $filename, $_ := .Changed.FileChanges -}}
      - {{ $filename }}
      {{ end -}}

      Objects:
      {{ range $resource, $changes := .Changed.Objects -}}
      - {{ $resource.Kind | lower }} {{ $resource.Name | lower }}
        Changes:
      {{- range $_, $change := $changes }}
        {{ if contains "5.0.3" $change.NewValue -}}
          - {{ $change.OldValue }} -> {{ $change.NewValue }}
        {{ else -}}
          [skip ci] wrong image
        {{ end -}}
      {{ end -}}
      {{ end -}}
```

There are over 70 available functions. Some of them are defined by the [Go
template language](https://pkg.go.dev/text/template) itself. Most of the others
are part of the [Sprig template library](http://masterminds.github.io/sprig/).  

Additional data can be provided with `.spec.git.commit.messageTemplateValues`.

This is a key/value mapping with string values.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  git:
    commit:
      messageTemplate: |-
        Automated image update by Flux for cluster {{ .Values.cluster }}.
      messageTemplateValues:
        cluster: prod
```

#### Push

`.spec.git.push` is an optional field that specifies how the commits are pushed
to the remote source repository.

##### Branch

`.spec.git.push.branch` field specifies the remote branch to push to. If
unspecified, the commits are pushed to the branch specified in
`.spec.git.checkout.branch`. If `.spec.git.checkout` is also unspecified, it
will fall back to the branch specified in the associated GitRepository's
`.spec.sourceRef`. If none of these yield a push branch name, the automation
will fail.

The push branch will be created locally if it does not already exist, starting
from the checkout branch. If the push branch already exists, it will be
overwritten with the cloned version plus the changes made by the controller.
Alternatively, force push can be disabled by starting the controller with flag
`--feature-gates=GitForcePushBranch=false`, in which case the updates will be
calculated on top of any commits already on the push branch. Note that without
force push in push branches, if the target branch is stale, the controller may
not be able to conclude the operation and will consistently fail until the
branch is either deleted or refreshed.

In the following snippet, updates will be pushed as commits to the branch
`auto`, and when that branch does not exist at the origin, it will be created
locally starting from the branch `main`, and pushed:

```yaml
spec:
  git:
    checkout:
      ref:
        branch: main
    push:
      branch: auto
```

##### Refspec

`.spec.git.push.refspec` field specifies the refspec to push to any arbitrary
destination reference. An example of a valid refspec is
`refs/heads/branch:refs/heads/branch`.

If both `.push.refspec` and `.push.branch` are specified, then the reconciler
will push to both the destinations. This is particularly useful for working with
Gerrit servers. For more information about this, please refer to the
[Gerrit](#gerrit) section. 

**Note:** If both `.push.refspec` and `.push.branch` are essentially equal to
each other (for e.g.: `.push.refspec: refs/heads/main:refs/heads/main` and
`.push.branch: main`), then the reconciler might fail with an `already
up-to-date` error.

In the following snippet, updates and commits will be made on the `main` branch locally.
The commits will be then pushed using the `refs/heads/main:refs/heads/auto` refspec:

```yaml
spec:
  git:
    checkout:
      ref:
        branch: main
    push:
      refspec: refs/heads/main:refs/heads/auto
```

##### Push options

To specify the [push options](https://git-scm.com/docs/git-push#Documentation/git-push.txt---push-optionltoptiongt)
to be sent to the upstream Git server, use `.push.options`. These options can be
used to perform operations as a result of the push. For example, using the below
push options will open a GitLab Merge Request to the `release` branch
automatically with the commit the controller pushed to the `dev` branch:

```yaml
spec:
  git:
    push:
      branch: dev
      options:
        merge_request.create: ""
        merge_request.target: release
```

### Interval

`.spec.interval` is a required field that specifies the interval at which the
Image update is attempted.

After successfully reconciling the object, the image-automation-controller
requeues it for inspection after the specified interval. The value must be in a
[Go recognized duration string format](https://pkg.go.dev/time#ParseDuration),
e.g. `10m0s` to reconcile the object every 10 minutes.

If the `.metadata.generation` of a resource changes (due to e.g. a change to
the spec), this is handled instantly outside the interval window.

### Update

`.spec.update` is an optional field that specifies how to carry out the updates
on a source. The only supported update strategy at the moment is `Setters`,
which is used by default for `.spec.update.strategy` field. The
`.spec.update.path` is an optional field to specify the directory containing the
manifests to be updated. If not specified, it defaults to the root of the source
repository.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  update:
    path: </path/to/manifest>
```

### Suspend

`.spec.suspend` is an optional field to suspend the reconciliation of an
ImageUpdateAutomation. When set to `true`, the controller will stop reconciling
the ImageUpdateAutomation, and changes to the resource or image policies or Git
repository will not result in any update. When the field is set to `false` or
removed, it will resume.

### PolicySelector

`.spec.policySelector` is an optional field to limit policies that an
ImageUpdateAutomation takes into account. It supports the same selectors as 
`Deployment.spec.selector` (`matchLabels` and `matchExpressions` fields). If
not specified, it defaults to `matchLabels: {}` which means all policies in
namespace.

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  policySelector:
    matchLabels:
      app.kubernetes.io/instance: my-app
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  policySelector:
    matchExpressions:
      - key: app.kubernetes.io/component
        operator: In
        values:
          - my-component
          - my-other-component
```

## Working with ImageUpdateAutomation

### Triggering a reconciliation

To manually tell the image-automation-controller to reconcile an
ImageUpdateAutomation outside of the [specified interval window](#interval), an
ImageUpdateAutomation can be annotated with
`reconcile.fluxcd.io/requestedAt: <arbitrary value>`. Annotating the resource
queues the ImageUpdateAutomation for reconciliation if the `<arbitrary-value>`
differs from the last value the controller acted on, as reported in
[`.status.lastHandledReconcileAt`](#last-handled-reconcile-at).

Using `kubectl`:

```sh
kubectl annotate --field-manager=flux-client-side-apply --overwrite imageupdateautomation/<automation-name> reconcile.fluxcd.io/requestedAt="$(date +%s)"
```

Using `flux`:

```sh
flux reconcile image update <automation-name>
```

### Waiting for `Ready`

When a change is applied, it is possible to wait for the ImageUpdateAutomation
to reach a [ready state](#ready-imageupdateautomation) using `kubectl`:

```sh
kubectl wait imageupdateautomation/<automation-name> --for=condition=ready --timeout=1m
```

### Suspending and resuming

When you find yourself in a situation where you temporarily want to pause the
reconciliation of a ImageUpdateAutomation, you can suspend it using the
[`.spec.suspend` field](#suspend).

#### Suspend an ImageUpdateAutomation

In your YAML declaration:

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  suspend: true
```

Using `kubectl`:

```sh
kubectl patch imageupdateautomation <automation-name> --field-manager=flux-client-side-apply -p '{\"spec\": {\"suspend\" : true }}'
```

Using `flux`:

```sh
flux suspend image update <automation-name>
```

#### Resume an ImageUpdateAutomation

In your YAML declaration, comment out (or remove) the `.spec.suspend` field:

```yaml
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: <automation-name>
spec:
  # suspend: true
```

**Note:** Setting the field value to `false` has the same effect as removing
it, but does not allow for "hot patching" using e.g. `kubectl` while practicing
GitOps; as the manually applied patch would be overwritten by the declared
state in Git.

Using `kubectl`:

```sh
kubectl patch imageupdateautomation <automation-name> --field-manager=flux-client-side-apply -p '{\"spec\" : {\"suspend\" : false }}'
```

Using `flux`:

```sh
flux resume image update <automation-name>
```

### Debugging an ImageUpdateAutomation

There are several ways to gather information about an ImageUpdateAutomation for
debugging purposes.

#### Describe the ImageUpdateAutomation

Describing an ImageUpdateAutomation using
`kubectl describe imageupdateautomation <automation-name>` displays the latest
recorded information for the resource in the `Status` and 
`Events` sections:

```console
...
Status:
  Conditions:
    Last Transition Time:     2024-03-18T20:00:56Z
    Message:                  processing object: new generation 6 -> 7
    Observed Generation:      7
    Reason:                   ProgressingWithRetry
    Status:                   True
    Type:                     Reconciling
    Last Transition Time:     2024-03-18T20:00:54Z
    Message:                  failed to checkout source: unable to clone 'https://github.com/fluxcd/example': couldn't find remote ref "refs/heads/non-existing-branch"
    Observed Generation:      7
    Reason:                   GitOperationFailed
    Status:                   False
    Type:                     Ready
  Last Automation Run Time:   2024-03-18T20:00:56Z
  Last Handled Reconcile At:  1710791381
  Last Push Commit:           8084f1bb180ac259c6698cd027064b7dce86a72a
  Last Push Time:             2024-03-18T18:53:04Z
  Observed Generation:        6
  Observed Policies:
    Podinfo - Policy:
      Name:  ghcr.io/stefanprodan/podinfo
      Tag:   4.0.6
  Observed Source Revision:  main@sha1:8084f1bb180ac259c6698cd027064b7dce86a72a
Events:
  Type     Reason              Age                  From                         Message
  ----     ------              ----                 ----                         -------
  Normal   Succeeded           11m (x11 over 170m)  image-automation-controller  no change since last reconciliation
  Warning  GitOperationFailed  2s (x3 over 4s)      image-automation-controller  failed to checkout source: unable to clone 'https://github.com/fluxcd/example': couldn't find remote ref "refs/heads/non-existing-branch"
```

#### Trace emitted Events

To view events for specific ImageUpdateAutomation(s), `kubectl events` can be
used in combination with `--for` to list the Events for specific objects. For
example, running

```sh
kubectl events --for ImageUpdateAutomation/<automation-name>
```

lists

```console
LAST SEEN               TYPE      REASON               OBJECT                                 MESSAGE
3m29s (x7 over 4m17s)   Warning   GitOperationFailed   ImageUpdateAutomation/<automation-name>   failed to checkout source: unable to clone 'https://github.com/fluxcd/example': couldn't find remote ref "refs/heads/non-existing-branch"
3m14s (x4 over 3h24m)   Normal    Succeeded            ImageUpdateAutomation/<automation-name>   repository up-to-date
2m41s (x12 over 174m)   Normal    Succeeded            ImageUpdateAutomation/<automation-name>   no change since last reconciliation
```

Besides being reported in Events, the reconciliation errors are also logged by
the controller. The Flux CLI offer commands for filtering the logs for a
specific ImageUpdateAutomation, e.g.
`flux logs --level=error --kind=ImageUpdateAutomation --name=<automation-name>`.

#### Gerrit

[Gerrit](https://www.gerritcodereview.com/) operates differently from a
standard Git server. Rather than sending individual commits to a branch,
all changes are bundled into a single commit. This commit requires a distinct
identifier separate from the commit SHA. Additionally, instead of initiating
a Pull Request between branches, the commit is pushed using a refspec:
`HEAD:refs/for/main`.

As the image-automation-controller is primarily designed to work with
standard Git servers, these special characteristics necessitate a few
workarounds. The following is an example configuration that works
well with Gerrit:

```yaml
spec:
  git:
    checkout:
      ref:
        branch: main
    commit:
      author:
        email: flux@localdomain
        name: flux
      messageTemplate: |
        Perform automatic image update

        Automation name: {{ .AutomationObject }}

        {{- $ChangeId := .AutomationObject -}}
        {{- $ChangeId = printf "%s%s" $ChangeId ( .Changed.FileChanges | toString ) -}}
        {{- $ChangeId = printf "%s%s" $ChangeId ( .Changed.Objects | toString ) -}}
        {{- $ChangeId = printf "%s%s" $ChangeId ( .Changed.Changes | toString ) }}
        Change-Id: {{ printf "I%s" ( sha256sum $ChangeId | trunc 40 ) }}
    push:
      branch: auto
      refspec: refs/heads/auto:refs/heads/main
```

This instructs the image-automation-controller to clone the repository using the
`main` branch but execute its update logic and commit with the provided message
template on the `auto` branch. Commits are then pushed to the `auto` branch,
followed by pushing the `HEAD` of the `auto` branch to the `HEAD` of the remote
`main` branch. The message template ensures the inclusion of a [Change-Id](https://gerrit-review.googlesource.com/Documentation/concept-changes.html#change-id)
at the bottom of the commit message.

The initial branch push aims to prevent multiple
[Patch Sets](https://gerrit-review.googlesource.com/Documentation/concept-patch-sets.html).
If we exclude `.push.branch` and only specify
`.push.refspec: refs/heads/main:refs/heads/main`, the desired [Change](https://gerrit-review.googlesource.com/Documentation/concept-changes.html)
can be created as intended. However, when the controller freshly clones the
`main` branch while a Change is open, it executes its update logic on `main`,
leading to new commits being pushed with the same changes to the existing open
Change. Specifying `.push.branch` circumvents this by instructing the controller
to apply the update logic to the `auto` branch, already containing the desired
commit. This approach is also recommended in the
[Gerrit documentation](https://gerrit-review.googlesource.com/Documentation/intro-gerrit-walkthrough-github.html#create-change).

Another thing to note is the syntax of `.push.refspec`. Instead of it being
`HEAD:refs/for/main`, commonly used by Gerrit users, we specify the full
refname `refs/heads/auto` in the source part of the refpsec.

**Note:** A known limitation of using the image-automation-controller with
Gerrit involves handling multiple concurrent Changes. This is due to the
calculation of the Change-Id, relying on factors like file names and image
tags. If the controller introduces a new file or modifies a previously updated
image tag to a different one, it leads to a distinct Change-Id for the commit.
Consequently, this action will trigger the creation of an additional Change,
even when an existing Change containing outdated modifications remains open.

## ImageUpdateAutomation Status

### Observed Policies

The ImageUpdateAutomation reports the observed image policies that were
considered during the image update in the `.status.observedPolicies` field. It
is a map of the policy name and its latest image name and tag.

Example:
```yaml
status:
  ...
  observedPolicies:
    podinfo-policy:
      name: ghcr.io/stefanprodan/podinfo
      tag: 4.0.6
    myapp1:
      name: ghcr.io/fluxcd/myapp1
      tag: 4.0.0
    myapp2:
      name: ghcr.io/fluxcd/myapp2
      tag: 2.0.0
  ...
```

The observed policies keep track of the policies considered in the last
reconciliation and is used to determine if the reconciliation can skip full
execution due to no change in image policies or remote source.

### Observed Source Revision

The ImageUpdateAutomation reports the observed source revision that was checked
out during the image update in the `.status.observedSourceRevision` field. For a
GitRepository, the observed source revision would contain the branch name and
the commit hash; e.g., `main@sha1:8084f1bb180ac259c6698cd027064b7dce86a72a`.
If the checkout and push branchs are the same, the commit hash of the observed
source revision is equal to the [last push commit](#last-push-commit).

The observed source revision keeps track of the source revision seen in the last
reconciliation and is used to determine if the reconciliation can skip full
execution due to no change in image policies or remote source.

### Last Automation Run Time

The ImageUpdateAutomation reports the last automation run time in the
`.status.lastAutomationRunTime` field. It is a timestamp of when the
reconciliation ran the last time, regardless of any effective resulting update.

### Last Push Commit

The ImageUpdateAutomation reports the last pushed commit for image update in the
`.status.lastPushCommit` field. It is the commit hash of the last pushed commit.
The commit has may not be the same that's present in the observed source
revision if the puch branch is different from the checkout branch or the remote
repository has new commits which didn't result in an image update.

### Last Push Time

The ImageUpdateAutomation reports the last pushed commit time for image update
in the `.status.lastPushTime` field. It is a timestamp of when the last image
update resulted in a pushing of new commit to the source.

### Conditions

An ImageUpdateAutomation enters various states during its lifecycle, reflected
as [Kubernetes Conditions][typical-status-properties].
It can be [reconciling](#reconciling-imageupdateautomation) while checking out
and updating images in source, it can be [ready](#ready-imageupdateautomation),
or it can [fail during reconciliation](#failed-imageupdateautomation).

The ImageUpdateAutomation API is compatible with the [kstatus specification][kstatus-spec],
and reports `Reconciling` and `Stalled` conditions where applicable to provide
better (timeout) support to solutions polling the ImageUpdateAutomation to
become `Ready`.

#### Reconciling ImageUpdateAutomation

The image-automation-controller marks an ImageUpdateAutomation as _reconciling_
when one of the following is true:

- The generation of the ImageUpdateAutomation is newer than the [Observed
Generation](#observed-generation).
- The ImageUpdateAutomation has observed new ImagePolicies or changes in the
  ImagePolicies' latest images, or change in the remote source.

When the ImageUpdateAutomation is "reconciling", the `Ready` Condition status
becomes `Unknown`, and the controller adds a Condition with the following
attributes to the ImageUpdateAutomation's `.status.conditions`:

- `type: Reconciling`
- `status: "True"`
- `reason: Progressing`

It has a ["negative polarity"][typical-status-properties], and is only present
on the ImageUpdateAutomation while its status value is `"True"`.

#### Ready ImageUpdateAutomation

The image-automation-controller marks an ImageUpdateAutomation as _ready_ when
it has the following characteristics:

- The controller was able to check out the remote source repository using the
  specified GitRepository configurations.
- The ImageUpdateAutomation could not find any update to the source, already
  up-to-date.
- The ImageUpdateAutomation pushes image updates to the source, making it
  up-to-date.

When the ImageUpdateAutomation is "ready", the controller sets a Condition with the
following attributes in the ImageUpdateAutomation's `.status.conditions`:

- `type: Ready`
- `status: "True"`
- `reason: Succeeded`

This `Ready` Condition will retain a status value of `"True"` until a
[failure](#failed-imageupdateautomation) occurs due to any reason.

#### Failed ImageUpdateAutomation

The image-automation-controller may get stuck trying to update a source without
completing. This can occur due to some of the following factors:

- The remote source is temporarily unavailable.
- The referenced source is in a different namespace and cross-namespace
  reference is disabled.
- The referenced source does not exist.
- The credentials associated with the source are invalid.
- The source configuration is invalid for the current state of the source, for
  example, the specified branch does not exists in the remote source repository.
- The remote source repository prevents push or creation of new push branch.
- The policy selector is invalid, for example, label is too long.

When this happens, the controller sets the `Ready` Condition status to `False`
with the following reasons:

- `reason: AccessDenied` | `reason: InvalidSourceConfiguration` | `reason: GitOperationFailed` | `reason: UpdateFailed` | `reason: InvalidPolicySelector`

While the ImageUpdateAutomation is in failing state, the controller will
continue to attempt to update the source with an exponential backoff, until it
succeeds and the ImageUpdateAutomation is marked as 
[ready](#ready-imageupdateautomation).

Note that an ImageUpdateAutomation can be [reconciling](#reconciling-imageupdateautomation)
while failing at the same time, for example due to a newly introduced
configuration issue in the ImageUpdateAutomation spec.

### Observed Generation

The image-automation-controller reports an
[observed generation][typical-status-properties] in the ImageUpdateAutomation's
`.status.observedGeneration`. The observed generation is the latest
`.metadata.generation` which resulted in either a
[ready state](#ready-imageupdateautomation), or stalled due to error it can not
recover from without human intervention.

### Last Handled Reconcile At

The image-automation-controller reports the last
`reconcile.fluxcd.io/requestedAt` annotation value it acted on in the
`.status.lastHandledReconcileAt` field.

For practical information about this field, see [triggering a
reconcile](#triggering-a-reconcile).


[image-auto-guide]: https://fluxcd.io/flux/guides/image-update/#configure-image-update-for-custom-resources
[go-text-template]: https://golang.org/pkg/text/template/
[typical-status-properties]: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
[kstatus-spec]:
    https://github.com/kubernetes-sigs/cli-utils/tree/master/pkg/kstatus
