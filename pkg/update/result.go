package update

import (
	"github.com/google/go-containerregistry/pkg/name"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

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
	// Policy gives the namespaced name of the image policy that led
	// to the update.
	Policy() types.NamespacedName
}

type imageRef struct {
	name.Reference
	policy types.NamespacedName
}

// Policy gives the namespaced name of the policy that led to the
// update.
func (i imageRef) Policy() types.NamespacedName {
	return i.policy
}

// Repository gives the repository component of the image ref.
func (i imageRef) Repository() string {
	return i.Context().RepositoryStr()
}

// Registry gives the registry component of the image ref.
func (i imageRef) Registry() string {
	return i.Context().Registry.String()
}

// ObjectIdentifier holds the identifying data for a particular
// object. This won't always have a name (e.g., a kustomization.yaml).
type ObjectIdentifier struct {
	yaml.ResourceIdentifier
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

// Images returns all the images that were involved in at least one
// update.
func (r Result) Images() []ImageRef {
	seen := make(map[ImageRef]struct{})
	var result []ImageRef
	for _, file := range r.Files {
		for _, images := range file.Objects {
			for _, ref := range images {
				if _, ok := seen[ref]; !ok {
					seen[ref] = struct{}{}
					result = append(result, ref)
				}
			}
		}
	}
	return result
}

// Objects returns a map of all the objects against the images updated
// within, regardless of which file they appear in.
func (r Result) Objects() map[ObjectIdentifier][]ImageRef {
	result := make(map[ObjectIdentifier][]ImageRef)
	for _, file := range r.Files {
		for res, refs := range file.Objects {
			result[res] = append(result[res], refs...)
		}
	}
	return result
}

// ResultV2 contains Result of update and also the file changes made during the
// update. This extends the Result to include details about the exact changes
// made to the files and the objects in them. It has a nested structure
// file->objects->changes.
type ResultV2 struct {
	ImageResult Result
	FileChanges map[string]ObjectChanges
}

// ObjectChanges contains all the changes made to objects.
type ObjectChanges map[ObjectIdentifier][]Change

// Change contains the setter that resulted in a Change, the old and the new
// value after the Change.
type Change struct {
	OldValue string
	NewValue string
	Setter   string
}

// AddChange adds changes to Resultv2 for a given file, object and changes
// associated with it.
func (r *ResultV2) AddChange(file string, objectID ObjectIdentifier, changes ...Change) {
	if r.FileChanges == nil {
		r.FileChanges = map[string]ObjectChanges{}
	}
	// Create an entry for the file if not present.
	_, ok := r.FileChanges[file]
	if !ok {
		r.FileChanges[file] = ObjectChanges{}
	}
	// Append to the changes for the object.
	r.FileChanges[file][objectID] = append(r.FileChanges[file][objectID], changes...)
}

// Changes returns all the changes that were made in at least one update.
func (r ResultV2) Changes() []Change {
	seen := make(map[Change]struct{})
	var result []Change
	for _, objChanges := range r.FileChanges {
		for _, changes := range objChanges {
			for _, change := range changes {
				if _, ok := seen[change]; !ok {
					seen[change] = struct{}{}
					result = append(result, change)
				}
			}
		}
	}
	return result
}

// Objects returns ObjectChanges, regardless of which file they appear in.
func (r ResultV2) Objects() ObjectChanges {
	result := make(ObjectChanges)
	for _, objChanges := range r.FileChanges {
		for obj, change := range objChanges {
			result[obj] = change
		}
	}
	return result
}
