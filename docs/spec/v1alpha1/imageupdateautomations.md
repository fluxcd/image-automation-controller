<!-- -*- fill-column: 100 -*- -->
# Image Update Automations

The `ImageUpdateAutomation` type defines an automation process that will update a git repository,
based on image policiy objects in the same namespace.

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
	// the repository
	// +required
	Update UpdateStrategy `json:"update"`
	// Commit specifies how to commit to the git repo
	// +required
	Commit CommitSpec `json:"commit"`

	// Suspend tells the controller to not run this automation, until
	// it is unset (or set to false). Defaults to false.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}
```

See the sections below, regarding `checkout`, `update`, and `commit`.

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

The `branch` field names the branch in the git repository to check out; this will also be the branch
the controller pushes commits to.

**Git implementation**

The `gitImplementation` field controls which git library is used. This will matter if you run on
Azure, and possibly otherwise -- see [the source controller documentation][source-docs] for more
details.

## Update strategy

The `update` field specifies how to carry out updates on the git repository:

```go
// UpdateStrategy is a union of the various strategies for updating
// the git repository.
type UpdateStrategy struct {
	// Setters if present means update workloads using setters, via
	// fields marked in the files themselves.
	// +optional
	Setters *SettersStrategy `json:"setters,omitempty"`
}

// SettersStrategy specifies how to use kyaml setters to update the
// git repository.
type SettersStrategy struct {
}
```

At present, there is one strategy: "Setters". This uses field markers referring to image policies,
as described in the [image automation guide][image-auto-guide]. Since the setters policy has no
fields itself, but a value is required, a full update strategy looks like this:

```yaml
spec:
  # ... checkout, interval etc.
  update:
    setters: {}
```

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

The `messageTemplate` field is a string which will be used as the commit message. If empty, there is
a default message; but you will likely want to provide your own, especially if you want to put
tokens in to control how CI reacts to commits made by automation. For example,

```yaml
spec:
  commit:
    messsageTemplate: |
      Automated image update by Flux
      
      [ci skip]
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
