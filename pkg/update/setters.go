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

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/name"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/kustomize/kyaml/fieldmeta"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
	"sigs.k8s.io/kustomize/kyaml/openapi"
	"sigs.k8s.io/kustomize/kyaml/sets"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/fluxcd/image-automation-controller/internal/constants"
	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
)

const (
	// This is preserved from setters2
	K8sCliExtensionKey = "x-k8s-cli"
)

func init() {
	fieldmeta.SetShortHandRef(constants.SetterShortHand)
	// this prevents the global schema, should it be initialised, from
	// parsing all the Kubernetes openAPI definitions, which is not
	// necessary.
	openapi.SuppressBuiltInSchemaUse()
}

// UpdateWithSetters takes all YAML files from `inpath`, updates any
// that contain an "in scope" image policy marker, and writes files it
// updated (and only those files) back to `outpath`. It also returns the result
// of the changes it made as Result.
func UpdateWithSetters(tracelog logr.Logger, inpath, outpath string, policies []imagev1_reflect.ImagePolicy) (Result, error) {
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
	var result Result

	// Compilng the result needs the file, the image ref used, and the
	// object. Each setter will supply its own name to its callback,
	// which can be used to look up the image ref; the file and object
	// we will get from `setAll` which keeps track of those as it
	// iterates.
	imageRefs := make(map[string]imageRef)
	setAllCallback := func(file, setterName string, node *yaml.RNode, old, new string) {
		_, ok := imageRefs[setterName]
		if !ok {
			return
		}

		meta, err := node.GetMeta()
		if err != nil {
			return
		}
		oid := ObjectIdentifier{meta.GetIdentifier()}

		// Record the change.
		ch := Change{
			OldValue: old,
			NewValue: new,
			Setter:   setterName,
		}
		// Append the change for the file and identifier.
		result.AddChange(file, oid, ch)
	}

	defs := map[string]spec.Schema{}
	for _, policy := range policies {
		if policy.Status.LatestRef == nil {
			continue
		}
		// Using strict validation would mean any image that omits the
		// registry would be rejected, so that can't be used
		// here. Using _weak_ validation means that defaults will be
		// filled in. Usually this would mean the tag would end up
		// being `latest` if empty in the input; but I'm assuming here
		// that the policy won't have a tagless ref.
		image := policy.Status.LatestRef.String()
		r, err := name.ParseReference(image, name.WeakValidation)
		if err != nil {
			return Result{}, fmt.Errorf("encountered invalid image ref %q: %w", image, err)
		}
		ref := imageRef{
			Reference: r,
			policy: types.NamespacedName{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			},
		}

		tag := policy.Status.LatestRef.Tag
		name := policy.Status.LatestRef.Name
		digest := policy.Status.LatestRef.Digest

		imageSetter := fmt.Sprintf("%s:%s", policy.GetNamespace(), policy.GetName())
		tracelog.Info("adding setter", "name", imageSetter)
		defs[fieldmeta.SetterDefinitionPrefix+imageSetter] = setterSchema(imageSetter, image)
		imageRefs[imageSetter] = ref

		tagSetter := imageSetter + ":tag"
		tracelog.Info("adding setter", "name", tagSetter)
		defs[fieldmeta.SetterDefinitionPrefix+tagSetter] = setterSchema(tagSetter, tag)
		imageRefs[tagSetter] = ref

		nameSetter := imageSetter + ":name"
		tracelog.Info("adding setter", "name", nameSetter)
		defs[fieldmeta.SetterDefinitionPrefix+nameSetter] = setterSchema(nameSetter, name)
		imageRefs[nameSetter] = ref

		digestSetter := imageSetter + ":digest"
		tracelog.Info("adding setter", "name", digestSetter)
		defs[fieldmeta.SetterDefinitionPrefix+digestSetter] = setterSchema(digestSetter, digest)
		imageRefs[digestSetter] = ref
	}

	settersSchema.Definitions = defs

	// get ready with the reader and writer
	reader := &ScreeningLocalReader{
		Path:  inpath,
		Token: fmt.Sprintf("%q", constants.SetterShortHand),
		Trace: tracelog,
	}
	writer := &kio.LocalPackageWriter{
		PackagePath: outpath,
	}

	pipeline := kio.Pipeline{
		Inputs:  []kio.Reader{reader},
		Outputs: []kio.Writer{writer},
		Filters: []kio.Filter{
			setAll(&settersSchema, tracelog, setAllCallback),
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
func setAll(schema *spec.Schema, tracelog logr.Logger, callback func(file, setterName string, node *yaml.RNode, old, new string)) kio.Filter {
	filter := &SetAllCallback{
		SettersSchema: schema,
		Trace:         tracelog,
	}
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
						callback(path, setter, nodes[i], oldValue, newValue)
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
	schema.Extensions.Add(K8sCliExtensionKey, map[string]interface{}{
		"setter": map[string]string{
			"name":  name,
			"value": value,
		},
	})
	return *schema
}
