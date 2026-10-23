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
	"cmp"
	"maps"
	"slices"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"
)

// ObjectIdentifier holds the identifying data for a particular
// object. This won't always have a name (e.g., a kustomization.yaml).
type ObjectIdentifier struct {
	yaml.ResourceIdentifier
}

// Result contains the file changes made during the update. It contains
// details about the exact changes made to the files and the objects in them.
// It has a nested structure file->objects->changes.
type Result struct {
	FileChanges map[string]ObjectChanges

	// images maps changed setter names to copies of their policies' latest refs.
	images map[string]reflectorv1.ImageRef
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

// AddChange adds changes to Result for a given file, object and changes
// associated with it.
func (r *Result) AddChange(file string, objectID ObjectIdentifier, changes ...Change) {
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

// addImage records the policy's latest reference for a setter that made a change.
func (r *Result) addImage(setter string, ref reflectorv1.ImageRef) {
	if r.images == nil {
		r.images = make(map[string]reflectorv1.ImageRef)
	}
	r.images[setter] = ref
}

// Summary returns a description of the changes, one line per distinct change,
// sorted and joined by newlines, without a trailing newline. A change made by a
// full-reference setter is described as "<old> -> <new>". A change made by a
// tag, name or digest setter is prefixed with the policy's image name and the
// field, e.g. "<image> tag <old> -> <new>"; the digest label also carries the
// policy's tag. Values are printed as written to the file. A result without
// changes gives an empty string.
func (r Result) Summary() string {
	var lines []string
	for _, change := range r.Changes() {
		line := change.OldValue + " -> " + change.NewValue
		parts := strings.Split(change.Setter, ":")
		if image, ok := r.images[change.Setter]; ok && len(parts) == 3 {
			switch parts[2] {
			case "tag", "name":
				line = image.Name + " " + parts[2] + " " + line
			case "digest":
				label := image.Name
				if image.Tag != "" {
					label += ":" + image.Tag
				}
				line = label + " digest " + line
			}
		}
		lines = append(lines, line)
	}
	slices.Sort(lines)
	return strings.Join(slices.Compact(lines), "\n")
}

// Changes returns all the changes that were made in at least one update,
// ordered by file name, then object, then the order within the object.
func (r Result) Changes() []Change {
	seen := make(map[Change]struct{})
	var result []Change
	for _, file := range slices.Sorted(maps.Keys(r.FileChanges)) {
		objChanges := r.FileChanges[file]
		for _, obj := range sortedObjects(objChanges) {
			for _, change := range objChanges[obj] {
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
// Changes of an object found in several files are ordered by file name.
func (r Result) Objects() ObjectChanges {
	result := make(ObjectChanges)
	for _, file := range slices.Sorted(maps.Keys(r.FileChanges)) {
		for obj, change := range r.FileChanges[file] {
			result[obj] = append(result[obj], change...)
		}
	}
	return result
}

// sortedObjects returns the object identifiers of the given changes in
// a stable order, so that results derived from them are deterministic.
func sortedObjects(oc ObjectChanges) []ObjectIdentifier {
	return slices.SortedFunc(maps.Keys(oc), func(a, b ObjectIdentifier) int {
		return cmp.Or(
			cmp.Compare(a.APIVersion, b.APIVersion),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})
}
