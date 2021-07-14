<!-- -*- fill-column: 100 -*- -->
# Image Update Automations

The `ImageUpdateAutomation` type defines an automation process that will update a git repository,
based on image policy objects in the same namespace.

The updates are governed by marking fields to be updated in each YAML file. For each field marked,
the automation process checks the image policy named, and updates the field value if there is a new
image selected by the policy. The marker format is shown in the [image automation
guide][image-auto-guide].

To see what has changed between the API version v1alpha1 and this version v1alpha2, read [the section on migration](#migrating-from-v1alpha1) at the bottom.

## Specification

```go
// ImageUpdateAutomationSpec defines the desired state of ImageUpdateAutomation
type ImageUpdateAutomationSpec struct {
	// SourceRef refers to the resource giving access details
	// to a git repository.
	// +required
	SourceRef SourceReference `json:"sourceRef"`
	// GitSpec contains all the git-specific definitions. This is
	// technically optional, but in practice mandatory until there are
	// other kinds of source allowed.
	// +optional
	GitSpec *GitSpec `json:"git,omitempty"`

	// Interval gives an lower bound for how often the automation
	// run should be attempted.
	// +required
	Interval metav1.Duration `json:"interval"`

	// Update gives the specification for how to update the files in
	// the repository. This can be left empty, to use the default
	// value.
	// +kubebuilder:default={"strategy":"Setters"}
	Update *UpdateStrategy `json:"update,omitempty"`

	// Suspend tells the controller to not run this automation, until
	// it is unset (or set to false). Defaults to false.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}
```

The `sourceRef` field refers to the `GitRepository` object that has details on how to access the Git
repository to be updated. The `kind` field in the reference currently only supports the value
`GitRepository`, which is the default.

```go
// SourceReference contains enough information to let you locate the
// typed, referenced source object.
type SourceReference struct {
	// API version of the referent
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Kind of the referent
	// +kubebuilder:validation:Enum=GitRepository
	// +kubebuilder:default=GitRepository
	// +required
	Kind string `json:"kind"`

	// Name of the referent
	// +required
	Name string `json:"name"`
}
```

To be able to commit changes back, the referenced `GitRepository` object must refer to credentials
with write access; e.g., if using a GitHub deploy key, "Allow write access" should be checked when
creating it. Only the `url`, `ref`, and `secretRef` fields of the `GitRepository` are used.

The [`gitImplementation` field][source-docs] in the referenced `GitRepository` is ignored. The
automation controller cannot use shallow clones or submodules, so there is no reason to use the
go-git implementation rather than libgit2.

Other fields particular to how the Git repository is used are in the `git` field, [described
below](#git-specific-specification).

The `update` field is described in [its own section below](update-strategy).

The required field `interval` gives a period for automation runs, in [duration notation][durations];
e.g., `"5m"`.

While `suspend` has a value of `true`, the automation will not run.

## Git-specific specification

The `git` field has this definition:

```go
type GitSpec struct {
	// Checkout gives the parameters for cloning the git repository,
	// ready to make changes. If not present, the `spec.ref` field from the
	// referenced `GitRepository` or its default will be used.
	// +optional
	Checkout *GitCheckoutSpec `json:"checkout,omitempty"`

	// Commit specifies how to commit to the git repository.
	// +required
	Commit CommitSpec `json:"commit"`

	// Push specifies how and where to push commits made by the
	// automation. If missing, commits are pushed (back) to
	// `.spec.checkout.branch` or its default.
	// +optional
	Push *PushSpec `json:"push,omitempty"`
}
```

The fields `checkout`, `commit` and `push` are explained in the following sections.

### Checkout

The optional `.spec.git.checkout` gives the Git reference to check out. The `.ref` value is the same
format as the `.ref` field in a [`GitRepository`][git-repo-ref].

```go
type GitCheckoutSpec struct {
	// Reference gives a branch, tag or commit to clone from the Git
	// repository.
	// +required
	Reference sourcev1.GitRepositoryRef `json:"ref"`
}
```

When `checkout` is given, it overrides the analogous field in the `GitRepository` object referenced
in `.spec.sourceRef`. You would use this to put automation commits on a different branch than that
you are syncing, for example.

### Commit

The `.spec.git.commit` field gives details to use when making a commit to push to the Git repository:

```go
// CommitSpec specifies how to commit changes to the git repository
type CommitSpec struct {
	// Author gives the email and optionally the name to use as the
	// author of commits.
	// +required
	Author CommitUser `json:"author"`
	// SigningKey provides the option to sign commits with a GPG key
	// +optional
	SigningKey *SigningKey `json:"signingKey,omitempty"`
	// MessageTemplate provides a template for the commit message,
	// into which will be interpolated the details of the change made.
	// +optional
	MessageTemplate string `json:"messageTemplate,omitempty"`
}

type CommitUser struct {
	// Name gives the name to provide when making a commit.
	// +optional
	Name string `json:"name,omitempty"`
	// Email gives the email to provide when making a commit.
	// +required
	Email string `json:"email"`
}

// SigningKey references a Kubernetes secret that contains a GPG keypair
type SigningKey struct {
	// SecretRef holds the name to a secret that contains a 'git.asc' key
	// corresponding to the ASCII Armored file containing the GPG signing
	// keypair as the value. It must be in the same namespace as the
	// ImageUpdateAutomation.
	// +required
	SecretRef meta.LocalObjectReference `json:"secretRef,omitempty"`
}
```

The `author` field gives the author for commits. For example,

```yaml
spec:
  git:
    commit:
      author:
        name: Fluxbot
        email: flux@example.com
```

will result in commits with the author `Fluxbot <flux@example.com>`.

The optional `signingKey` field can be used to provide a key to sign commits with. It holds a
reference to a secret, which is expected to have a file called `git.asc` containing an
ASCII-armoured PGP key.

The `messageTemplate` field is a string which will be used as a template for the commit message. If
empty, there is a default message; but you will likely want to provide your own, especially if you
want to put tokens in to control how CI reacts to commits made by automation. For example,

```yaml
spec:
  git:
    commit:
      messageTemplate: |
        Automated image update by Flux
        
        [ci skip]
```

The following section describes what data is available to use in the template.

#### Commit message template data

The message template is a [Go text template][go-text-template]. The data available to the template
have this structure (not reproduced verbatim):

```go
// controllers/imageupdateautomation_controller.go

// TemplateData is the type of the value given to the commit message
// template.
type TemplateData struct {
	AutomationObject struct {
      Name, Namespace string
    }
	Updated          update.Result
}

// pkg/update/result.go

// ImageRef represents the image reference used to replace a field
// value in an update.
type ImageRef interface {
	// String returns a string representation of the image ref as it
	// is used in the update; e.g., "helloworld:v1.0.1"
	String() string
	// Identifier returns the tag or digest; e.g., "v1.0.1"
	Identifier() string
	// Repository returns the repository component of the ImageRef,
	// with an implied defaults, e.g., "library/helloworld"
	Repository() string
	// Registry returns the registry component of the ImageRef, e.g.,
	// "index.docker.io"
	Registry() string
	// Name gives the fully-qualified reference name, e.g.,
	// "index.docker.io/library/helloworld:v1.0.1"
	Name() string
}

// ObjectIdentifier holds the identifying data for a particular
// object. This won't always have a name (e.g., a kustomization.yaml).
type ObjectIdentifier struct {
	Name, Namespace, APIVersion, Kind string
}

// Result reports the outcome of an automated update. It has a nested
// structure file->objects->images. Different projections (e.g., all
// the images, regardless of object) are available via methods.
type Result struct {
	Files map[string]FileResult
}

// FileResult gives the updates in a particular file.
type FileResult struct {
	Objects map[ObjectIdentifier][]ImageRef
}
```

These methods are defined on `update.Result`:

```go
// Images returns all the images that were involved in at least one
// update.
func (r Result) Images() []ImageRef {
    // ...
}

// Objects returns a map of all the objects against the images updated
// within, regardless of which file they appear in.
func (r Result) Objects() map[ObjectIdentifier][]ImageRef {
    // ...
}
```

The methods let you range over the objects and images without descending the data structure. Here's
an example of using the fields and methods in a template:

```yaml
spec:
  commit:
    messageTemplate: |
      Automated image update
      
      Automation name: {{ .AutomationObject }}
      
      Files:
      {{ range $filename, $_ := .Updated.Files -}}
      - {{ $filename }}
      {{ end -}}
      
      Objects:
      {{ range $resource, $_ := .Updated.Objects -}}
      - {{ $resource.Kind }} {{ $resource.Name }}
      {{ end -}}
      
      Images:
      {{ range .Updated.Images -}}
      - {{.}}
      {{ end -}}
```

### Push

The optional `push` field defines how commits are pushed to the origin.

```go
// PushSpec specifies how and where to push commits.
type PushSpec struct {
	// Branch specifies that commits should be pushed to the branch
	// named. The branch is created using `.spec.checkout.branch` as the
	// starting point, if it doesn't already exist.
	// +required
	Branch string `json:"branch"`
}
```

If `push` is not present, commits are made on the branch given in `.spec.git.checkout.branch` and
pushed to the same branch at the origin. If `.spec.git.checkout` is not present, it will fall back
to the branch given in the `GitRepository` referenced by `.spec.sourceRef`. If none of these yield a
branch name, the automation will fail.

When `push` is present, the `branch` field specifies a branch to push to at the origin. The branch
will be created locally if it does not already exist, starting from the checkout branch. If it does
already exist, updates will be calculated on top of any commits already on the branch.

In the following snippet, updates will be pushed as commits to the branch `auto`, and when that
branch does not exist at the origin, it will be created locally starting from the branch `main`, and
pushed:

```yaml
spec:
  git:
    checkout:
      ref:
        branch: main
    push:
      branch: auto
```

## Update strategy

The `.spec.update` field specifies how to carry out updates on the git repository. There is one
strategy possible at present -- `{strategy: Setters}`. This field may be left empty, to default to
that value.

```go
// UpdateStrategyName is the type for names that go in
// .update.strategy. NB the value in the const immediately below.
// +kubebuilder:validation:Enum=Setters
type UpdateStrategyName string

const (
	// UpdateStrategySetters is the name of the update strategy that
	// uses kyaml setters. NB the value in the enum annotation for the
	// type, above.
	UpdateStrategySetters UpdateStrategyName = "Setters"
)

// UpdateStrategy is a union of the various strategies for updating
// the Git repository. Parameters for each strategy (if any) can be
// inlined here.
type UpdateStrategy struct {
	// Strategy names the strategy to be used.
	// +required
	// +kubebuilder:default=Setters
	Strategy UpdateStrategyName `json:"strategy"`

	// Path to the directory containing the manifests to be updated.
	// Defaults to 'None', which translates to the root path
	// of the GitRepositoryRef.
	// +optional
	Path string `json:"path,omitempty"`
}
```

**Setters strategy**

At present, there is one strategy: "Setters". This uses field markers referring to image policies,
as described in the [image automation guide][image-auto-guide].

## Status

The status of an `ImageUpdateAutomation` object records the result of the last automation run.

```go
// ImageUpdateAutomationStatus defines the observed state of ImageUpdateAutomation
type ImageUpdateAutomationStatus struct {
	// LastAutomationRunTime records the last time the controller ran
	// this automation through to completion (even if no updates were
	// made).
	// +optional
	LastAutomationRunTime *metav1.Time `json:"lastAutomationRunTime,omitempty"`
	// LastPushCommit records the SHA1 of the last commit made by the
	// controller, for this automation object
	// +optional
	LastPushCommit string `json:"lastPushCommit,omitempty"`
	// LastPushTime records the time of the last pushed change.
	// +optional
	LastPushTime *metav1.Time `json:"lastPushTime,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Conditions                  []metav1.Condition `json:"conditions,omitempty"`
	meta.ReconcileRequestStatus `json:",inline"`
}
```

The `lastAutomationRunTime` gives the time of the last automation run, whether or not it made a
commit. The `lastPushCommit` field records the SHA1 hash of the last commit pushed to the origin git
repository, and the `lastPushTime` gives the time that push occurred.

### Conditions

There is one condition maintained by the controller, which is the usual `ReadyCondition`
condition. This will be recorded as `True` when automation has run without errors, whether or not it
resulted in a commit.

## Migrating from `v1alpha1`

For the most part, `v1alpha2` rearranges the API types to provide for future extension. Here are the
differences, and where each `v1alpha1` field goes. A full example appears after the table.

### Moves and changes

| `v1alpha1` field | change in `v1alpha2` |
|------------------|----------------------|
| .spec.checkout | moved to `.spec.git.checkout`, and optional |
|                | `gitRepositoryRef` is now `.spec.sourceRef` |
|                | `branch` is now `ref`, and optional         |
| .spec.commit   | moved to `.spec.git.commit` |
|                | `authorName` and `authorEmail` now `author.name` and `author.email` |
| .spec.push     | moved to `.spec.git.push` |

### Example of rewriting a v1alpha1 object to v1alpha2

This example shows the steps to rewrite a v1alpha1 ImageUpdateAutomation YAML to be a v1alpha2 YAML.

This is the v1alpha1 original:

```yaml
apiVersion: image.toolkit.fluxcd.io/v1alpha1
kind: ImageUpdateAutomation
spec:
  checkout:
    gitRepositoryRef:
      name: auto-repo
    branch: main
  interval: 5m
  # omit suspend, which has not changed
  update:
    strategy: Setters
    path: ./app
  commit:
    authorName: fluxbot
    authorEmail: fluxbot@example.com
    messageTemplate: |
      An automated update from FluxBot
      [ci skip]
    signingKey:
      secretRef:
        name: git-pgp
  push:
    branch: auto
```

**Change the API version**

The API version is now `image.toolkit.fluxcd.io/v1alpha2`:

```yaml
apiVersion: image.toolkit.fluxcd.io/v1alpha1

# becomes

apiVersion: image.toolkit.fluxcd.io/v1alpha2
```

**Move and adapt `.spec.checkout.gitRepositoryRef` to `.spec.sourceRef` and `.spec.git.checkout`**

The reference to a `GitRepository` object has moved to the field `sourceRef`. The `checkout` field
moves under the `git` key, with the branch to checkout in a structure under `ref`.

```yaml
spec:
  checkout:
    gitRepositoryRef:
      name: auto-repo
    branch:
      main

# becomes

spec:
  sourceRef:
    kind: GitRepository # the default, but good practice to be explicit here
    name: auto-repo
  git:
    checkout:
      ref:
        branch: main
```

Note that `.spec.git.checkout` is now optional. If not supplied, the `.spec.ref` field from the
`GitRepository` object is used as the checkout for updates.

**Move and adapt `.spec.commit` to `spec.git.commit`**

The `commit` field also moves under the `git` key, and the author is a structure rather than two
fields.

```yaml
spec:
  commit:
    authorName: fluxbot
    authorEmail: fluxbot@example.com
    messageTemplate: |
      An automated update from FluxBot
      [ci skip]
    signingKey:
      secretRef:
        name: git-pgp

# becomes

spec:
  git:
    commit:
      author:
        name: fluxbot
        email: fluxbot@example.com
      messageTemplate: |
        An automated update from FluxBot
        [ci skip]
      signingKey:
        secretRef:
          name: git-pgp
```

**Move `.spec.push` to `.spec.git.push`**

The field `push` moves under the `git` key.

```yaml
spec:
  push:
    branch: auto

# becomes

spec:
  git:
    push:
      branch: auto
```

**Overall result**

The final YAML looks like this:

```yaml
apiVersion: image.toolkit.fluxcd.io/v1alpha2
kind: ImageUpdateAutomation
spec:
  sourceRef: # moved from `.spec.checkout`
    kind: GitRepository
    name: auto-repo
  interval: 5m
  # omit suspend, which has not changed
  update:
    strategy: Setters
    path: ./app
  git:
    checkout: # moved under `git`, loses `gitRepositoryRef`
      ref:
        branch: main # moved into `ref` struct
    commit: # moved under `git`
      author:
        name: fluxbot  # moved from `authorName`
        email: fluxbot@example.com # moved from `authorEmail`
      messageTemplate: |
        An automated update from FluxBot
        [ci skip]
      signingKey:
        secretRef:
          name: git-pgp
    push: # moved under `git`
      branch: auto
```

[image-auto-guide]: https://toolkit.fluxcd.io/guides/image-update/#configure-image-update-for-custom-resources
[git-repo-ref]: https://toolkit.fluxcd.io/components/source/gitrepositories/#specification
[durations]: https://godoc.org/time#ParseDuration
[source-docs]: https://toolkit.fluxcd.io/components/source/gitrepositories/#git-implementation
[go-text-template]: https://golang.org/pkg/text/template/
