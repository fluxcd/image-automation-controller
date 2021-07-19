<!-- -*- fill-column: 100 -*- -->
# Image Update Automations

The `ImageUpdateAutomation` type defines an automation process that will update a git repository,
based on image policy objects in the same namespace.

The updates are governed by marking fields to be updated in each YAML file. For each field marked,
the automation process checks the image policy named, and updates the field value if there is a new
image selected by the policy. The marker format is shown in the [image automation
guide][image-auto-guide].

## Specification

```go
// ImageUpdateAutomationSpec defines the desired state of ImageUpdateAutomation
type ImageUpdateAutomationSpec struct {
	// Checkout gives the parameters for cloning the git repository,
	// ready to make changes.
	// +required
	Checkout GitCheckoutSpec `json:"checkout"`
	
	// Interval gives an lower bound for how often the automation
	// run should be attempted.
	// +required
	Interval metav1.Duration `json:"interval"`
	
	// Update gives the specification for how to update the files in
	// the repository. This can be left empty, to use the default
	// value.
	// +kubebuilder:default={"strategy":"Setters"}
	Update *UpdateStrategy `json:"update,omitempty"`
	
	// Commit specifies how to commit to the git repository.
	// +required
	Commit CommitSpec `json:"commit"`

	// Push specifies how and where to push commits made by the
	// automation. If missing, commits are pushed (back) to
	// `.spec.checkout.branch`.
	// +optional
	Push *PushSpec `json:"push,omitempty"`

	// Suspend tells the controller to not run this automation, until
	// it is unset (or set to false). Defaults to false.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}
```

See the sections below regarding `checkout`, `update`, `commit`, and `push`.

The required `interval` field gives a period for automation runs, in [duration notation][durations];
e.g., `"5m"`.

While `suspend` has a value of `true`, the automation will not run.

### Checkout

The checkout value specifies the git repository and branch in which to commit changes:

```go
type GitCheckoutSpec struct {
	// GitRepositoryRef refers to the resource giving access details
	// to a git repository to update files in.
	// +required
	GitRepositoryRef corev1.LocalObjectReference `json:"gitRepositoryRef"`
	// Branch gives the branch to clone from the git repository.
	// +required
	Branch string `json:"branch"`
}
```

The `gitRepositoryRef` field names a [`GitRepository`][git-repo-ref] object in the same
namespace. To be able to commit changes back, the `GitRepository` object must refer to credentials
with write access; e.g., if using a GitHub deploy key, "Allow write access" should be checked when
creating it. Only the `url`, `secretRef` and `gitImplementation` (see just below) fields of the
`GitRepository` are used.

The `branch` field names the branch in the git repository to check out. When the `push` field is not
present (see [below](#push)), this will also be the branch pushed back to the origin repository.

**Git implementation**

The `gitImplementation` field controls which git library is used. This will matter if you run on
Azure, and possibly otherwise -- see [the source controller documentation][source-docs] for more
details.

## Update strategy

The `update` field specifies how to carry out updates on the git repository. There is one strategy
possible at present -- `{strategy: Setters}`. This field may be left empty, to default to that
value.

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

## Commit

The commit field specifies how to construct a commit, once changes have been made to the files
according to the update strategy.

```go
// CommitSpec specifies how to commit changes to the git repository
type CommitSpec struct {
	// AuthorName gives the name to provide when making a commit
	// +required
	AuthorName string `json:"authorName"`
	
	// AuthorEmail gives the email to provide when making a commit
	// +required
	AuthorEmail string `json:"authorEmail"`
	
	// MessageTemplate provides a template for the commit message,
	// into which will be interpolated the details of the change made.
	// +optional
	MessageTemplate string `json:"messageTemplate,omitempty"`
	
	// SigningKey provides the option to sign commits with a GPG key
	// +optional
	SigningKey *SigningKey `json:"signingKey,omitempty"`
}
```

The `authorName` and `authorEmail` are used together to give the author of the commit. For example,

```yaml
spec:
  # checkout, update, etc.
  commit:
    authorName: Fluxbot
    authorEmail: flux@example.com
```

will result in commits with the author `Fluxbot <flux@example.com>`.

The `messageTemplate` field is a string which will be used as a template for the commit message. If
empty, there is a default message; but you will likely want to provide your own, especially if you
want to put tokens in to control how CI reacts to commits made by automation. For example,

```yaml
spec:
  commit:
    messageTemplate: |
      Automated image update by Flux
      
      [ci skip]
```

The `signingKey` field holds the reference to a secret that contains a `git.asc`
key corresponding to the ASCII Armored file containing the GPG signing keypair as the value.
For example,

```yaml
spec:
  commit:
    authorName: Fluxbot
    authorEmail: flux@example.com
    signingKey:
      secretRef:
        name: gpg-private-key
```

will result in commits with the author `Fluxbot <flux@example.com>` signed with the GPG key
present in the `gpg-private-key` secret.

### Commit template data

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

## Push

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

If `push` is not present, commits are made on the same branch as `.spec.checkout.branch` and pushed
to the same branch at the origin.

When `push` is present, the `.spec.push.branch` field specifies a branch to push to at the
origin. The branch will be created locally if it does not already exist, starting from
`.spec.checkout.branch`. If it does already exist, updates will be calculated on top of any commits
already on the branch.

In the following snippet, updates will be pushed as commits to the branch `auto`, and when that
branch does not exist at the origin, it will be created locally starting from the branch `main` and
pushed:

```yaml
spec:
  # ... commit, update, etc.
  checkout:
    gitRepositoryRef:
      name: app-repo
    branch: main
  push:
    branch: auto
```

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

[image-auto-guide]: https://toolkit.fluxcd.io/guides/image-update/#configure-image-update-for-custom-resources
[git-repo-ref]: https://toolkit.fluxcd.io/components/source/gitrepositories/
[durations]: https://godoc.org/time#ParseDuration
[source-docs]: https://toolkit.fluxcd.io/components/source/gitrepositories/#git-implementation
[go-text-template]: https://golang.org/pkg/text/template/
