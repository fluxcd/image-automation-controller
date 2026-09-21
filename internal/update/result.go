/*
Copyright 2025 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package update

import (
	"fmt"
	"sort"
	"strings"

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

// Result contains the file changes made during the update. It contains
// details about the exact changes made to the files and the objects in them.
// It has a nested structure file->objects->changes.
type Result struct {
	FileChanges FileChanges
}

// FileChanges contains all the object changes grouped by file.
type FileChanges map[string]ObjectChanges

// ObjectChanges contains all the changes made to objects.
type ObjectChanges map[ObjectIdentifier]Changes

// Changes contains the changes made to a single object or aggregated across
// multiple objects.
type Changes []Change

// Change contains the setter that resulted in a Change, the old and the new
// value after the Change.
type Change struct {
	OldValue string
	NewValue string
	Setter   string
}

// AddChange adds changes to Result for a given file, object and changes
// associated with it.
func (r *Result) AddChange(file string, objectID ObjectIdentifier, changes ...Change) {
	if r.FileChanges == nil {
		r.FileChanges = FileChanges{}
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
func (r Result) Changes() Changes {
	seen := make(map[Change]struct{})
	var result Changes
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
	sortChanges(result)
	return result
}

// Objects returns ObjectChanges, regardless of which file they appear in.
func (r Result) Objects() ObjectChanges {
	result := make(ObjectChanges)
	for _, objChanges := range r.FileChanges {
		for obj, change := range objChanges {
			result[obj] = append(result[obj], change...)
		}
	}
	for obj := range result {
		sortChanges(result[obj])
	}
	return result
}

// String returns a deterministic string representation of file changes.
func (fc FileChanges) String() string {
	files := make([]string, 0, len(fc))
	for file := range fc {
		files = append(files, file)
	}
	sort.Strings(files)

	var b strings.Builder
	b.WriteString("{")
	for i, file := range files {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%q:%s", file, fc[file])
	}
	b.WriteString("}")
	return b.String()
}

// String returns a deterministic string representation of object changes.
func (oc ObjectChanges) String() string {
	objectIDs := make([]ObjectIdentifier, 0, len(oc))
	for objectID := range oc {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Slice(objectIDs, func(i, j int) bool {
		return lessObjectIdentifier(objectIDs[i], objectIDs[j])
	})

	var b strings.Builder
	b.WriteString("{")
	for i, objectID := range objectIDs {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s:%s", objectID, sortedChanges(oc[objectID]))
	}
	b.WriteString("}")
	return b.String()
}

// String returns a deterministic string representation of changes.
func (c Changes) String() string {
	changes := sortedChanges(c)

	var b strings.Builder
	b.WriteString("[")
	for i, change := range changes {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "{OldValue:%q NewValue:%q Setter:%q}", change.OldValue, change.NewValue, change.Setter)
	}
	b.WriteString("]")
	return b.String()
}

func sortedChanges(changes Changes) Changes {
	sorted := append(Changes(nil), changes...)
	sortChanges(sorted)
	return sorted
}

func sortChanges(changes Changes) {
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Setter != changes[j].Setter {
			return changes[i].Setter < changes[j].Setter
		}
		if changes[i].OldValue != changes[j].OldValue {
			return changes[i].OldValue < changes[j].OldValue
		}
		return changes[i].NewValue < changes[j].NewValue
	})
}

func lessObjectIdentifier(a, b ObjectIdentifier) bool {
	aParts := []string{a.APIVersion, a.Kind, a.Namespace, a.Name}
	bParts := []string{b.APIVersion, b.Kind, b.Namespace, b.Name}
	for i := range aParts {
		if aParts[i] != bParts[i] {
			return aParts[i] < bParts[i]
		}
	}
	return false
}
