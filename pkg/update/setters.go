/*
Copyright 2020, 2021 The Flux authors

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

	"github.com/go-openapi/spec"
	"github.com/google/go-containerregistry/pkg/name"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kustomize/kyaml/fieldmeta"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
	"sigs.k8s.io/kustomize/kyaml/openapi"
	"sigs.k8s.io/kustomize/kyaml/sets"
	"sigs.k8s.io/kustomize/kyaml/setters2"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	imagev1alpha1_reflect "github.com/fluxcd/image-reflector-controller/api/v1alpha1"
)

const (
	// SetterShortHand is a shorthand that can be used to mark
	// setters; instead of
	// # { "$ref": "#/definitions/
	SetterShortHand = "$imagepolicy"
)

func init() {
	fieldmeta.SetShortHandRef(SetterShortHand)
	// this prevents the global schema, should it be initialised, from
	// parsing all the Kubernetes openAPI definitions, which is not
	// necessary.
	openapi.SuppressBuiltInSchemaUse()
}

// UpdateWithSetters takes all YAML files from `inpath`, updates any
// that contain an "in scope" image policy marker, and writes files it
// updated (and only those files) back to `outpath`.
func UpdateWithSetters(inpath, outpath string, policies []imagev1alpha1_reflect.ImagePolicy) (Result, error) {
	// the OpenAPI schema is a package variable in kyaml/openapi. In
	// lieu of being able to isolate invocations (per
	// https://github.com/kubernetes-sigs/kustomize/issues/3058), I
	// serialise access to it and reset it each time.

	// construct definitions

	// the format of the definitions expected is given here:
	//     https://github.com/kubernetes-sigs/kustomize/blob/master/kyaml/setters2/doc.go
	//
	//     {
	//        "definitions": {
	//          "io.k8s.cli.setters.replicas": {
	//            "x-k8s-cli": {
	//              "setter": {
	//                "name": "replicas",
	//                "value": "4"
	//              }
	//            }
	//          }
	//        }
	//      }
	//
	// (there are consts in kyaml/fieldmeta with the
	// prefixes).
	//
	// `fieldmeta.SetShortHandRef("$imagepolicy")` makes it possible
	// to just use (e.g.,)
	//
	//     image: foo:v1 # {"$imagepolicy": "automation-ns:foo"}
	//
	// to mark the fields at which to make replacements. A colon is
	// used to separate namespace and name in the key, because a slash
	// would be interpreted as part of the $ref path.

	var settersSchema spec.Schema

	// collect setter defs and setters by going through all the image
	// policies available.
	result := Result{
		Files: make(map[string]FileResult),
	}

	// Compilng the result needs the file, the image ref used, and the
	// object. Each setter will supply its own name to its callback,
	// which can be used to look up the image ref; the file and object
	// we will get from `setAll` which keeps track of those as it
	// iterates.
	imageRefs := make(map[string]imageRef)
	setAllCallback := func(file, setterName string, node *yaml.RNode) {
		ref, ok := imageRefs[setterName]
		if !ok {
			return
		}

		meta, err := node.GetMeta()
		if err != nil {
			return
		}
		oid := ObjectIdentifier{meta.GetIdentifier()}

		fileres, ok := result.Files[file]
		if !ok {
			fileres = FileResult{
				Objects: make(map[ObjectIdentifier][]ImageRef),
			}
			result.Files[file] = fileres
		}
		objres, ok := fileres.Objects[oid]
		for _, n := range objres {
			if n == ref {
				return
			}
		}
		objres = append(objres, ref)
		fileres.Objects[oid] = objres
	}

	defs := map[string]spec.Schema{}
	for _, policy := range policies {
		if policy.Status.LatestImage == "" {
			continue
		}
		// Using strict validation would mean any image that omits the
		// registry would be rejected, so that can't be used
		// here. Using _weak_ validation means that defaults will be
		// filled in. Usually this would mean the tag would end up
		// being `latest` if empty in the input; but I'm assuming here
		// that the policy won't have a tagless ref.
		image := policy.Status.LatestImage
		r, err := name.ParseReference(image, name.WeakValidation)
		if err != nil {
			return Result{}, fmt.Errorf("encountered invalid image ref %q: %w", policy.Status.LatestImage, err)
		}
		ref := imageRef{
			Reference: r,
			policy: types.NamespacedName{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			},
		}

		tag := ref.Identifier()
		// annoyingly, neither the library imported above, nor an
		// alternative I found, will yield the original image name;
		// this is an easy way to get it
		name := image[:len(tag)+1]

		imageSetter := fmt.Sprintf("%s:%s", policy.GetNamespace(), policy.GetName())
		defs[fieldmeta.SetterDefinitionPrefix+imageSetter] = setterSchema(imageSetter, policy.Status.LatestImage)
		imageRefs[imageSetter] = ref

		tagSetter := imageSetter + ":tag"
		defs[fieldmeta.SetterDefinitionPrefix+tagSetter] = setterSchema(tagSetter, tag)
		imageRefs[tagSetter] = ref

		// Context().Name() gives the image repository _as supplied_
		nameSetter := imageSetter + ":name"
		defs[fieldmeta.SetterDefinitionPrefix+nameSetter] = setterSchema(nameSetter, name)
		imageRefs[nameSetter] = ref
	}

	settersSchema.Definitions = defs
	set := &SetAllCallback{
		SettersSchema: &settersSchema,
	}

	// get ready with the reader and writer
	reader := &ScreeningLocalReader{
		Path:  inpath,
		Token: fmt.Sprintf("%q", SetterShortHand),
	}
	writer := &kio.LocalPackageWriter{
		PackagePath: outpath,
	}

	pipeline := kio.Pipeline{
		Inputs:  []kio.Reader{reader},
		Outputs: []kio.Writer{writer},
		Filters: []kio.Filter{
			setAll(set, setAllCallback),
		},
	}

	// go!
	err := pipeline.Execute()
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// setAll returns a kio.Filter using the supplied SetAllCallback
// (dealing with individual nodes), amd calling the given callback
// whenever a field value is changed, and returning only nodes from
// files with changed nodes. This is based on
// [`SetAll`](https://github.com/kubernetes-sigs/kustomize/blob/kyaml/v0.10.16/kyaml/setters2/set.go#L503
// from kyaml/kio.
func setAll(filter *SetAllCallback, callback func(file, setterName string, node *yaml.RNode)) kio.Filter {
	return kio.FilterFunc(
		func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
			filesToUpdate := sets.String{}
			for i := range nodes {
				path, _, err := kioutil.GetFileAnnotations(nodes[i])
				if err != nil {
					return nil, err
				}

				filter.Callback = func(setter, oldValue, newValue string) {
					if newValue != oldValue {
						callback(path, setter, nodes[i])
						filesToUpdate.Insert(path)
					}
				}
				_, err = filter.Filter(nodes[i])
				if err != nil {
					return nil, err
				}
			}

			var nodesInUpdatedFiles []*yaml.RNode
			for i := range nodes {
				path, _, err := kioutil.GetFileAnnotations(nodes[i])
				if err != nil {
					return nil, err
				}
				if filesToUpdate.Has(path) {
					nodesInUpdatedFiles = append(nodesInUpdatedFiles, nodes[i])
				}
			}
			return nodesInUpdatedFiles, nil
		})
}

func setterSchema(name, value string) spec.Schema {
	schema := spec.StringProperty()
	schema.Extensions = map[string]interface{}{}
	schema.Extensions.Add(setters2.K8sCliExtensionKey, map[string]interface{}{
		"setter": map[string]string{
			"name":  name,
			"value": value,
		},
	})
	return *schema
}
