package update

import (
	"github.com/google/go-containerregistry/pkg/name"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// Result reports the outcome of an update. It has a
// file->objects->images structure, i.e., from the top level down to the
// most detail. Different projections (e.g., all the images) are
// available via the methods.
type Result struct {
	Files map[string]FileResult
}

type FileResult struct {
	Objects map[yaml.ResourceIdentifier][]name.Reference
}

// Images returns all the images that were involved in at least one
// update.
func (r Result) Images() []name.Reference {
	seen := make(map[name.Reference]struct{})
	var result []name.Reference
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
func (r Result) Objects() map[yaml.ResourceIdentifier][]name.Reference {
	result := make(map[yaml.ResourceIdentifier][]name.Reference)
	for _, file := range r.Files {
		for res, refs := range file.Objects {
			result[res] = append(result[res], refs...)
		}
	}
	return result
}
