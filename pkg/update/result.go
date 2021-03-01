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
