/*
Copyright 2020 The Flux authors

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
	"sync"

	"github.com/go-openapi/spec"
	"github.com/google/go-containerregistry/pkg/name"
	"sigs.k8s.io/kustomize/kyaml/fieldmeta"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/openapi"
	"sigs.k8s.io/kustomize/kyaml/setters2"

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
}

var (
	// used to serialise access to the global schema, which needs to
	// be reset for each run
	schemaMu = &sync.Mutex{}
)

func resetSchema() {
	openapi.ResetOpenAPI()
	openapi.SuppressBuiltInSchemaUse()
}

func UpdateWithSetters(inpath, outpath string, policies []imagev1alpha1_reflect.ImagePolicy) error {
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
		ref, err := name.ParseReference(image, name.WeakValidation)
		if err != nil {
			return fmt.Errorf("encountered invalid image ref %q: %w", policy.Status.LatestImage, err)
		}
		tag := ref.Identifier()
		// annoyingly, neither the library imported above, nor an
		// alternative, I found will yield the original image name;
		// this is an easy way to get it
		name := image[:len(tag)+1]

		imageSetter := fmt.Sprintf("%s:%s", policy.GetNamespace(), policy.GetName())
		defs[fieldmeta.SetterDefinitionPrefix+imageSetter] = setterSchema(imageSetter, policy.Status.LatestImage)
		tagSetter := imageSetter + ":tag"
		defs[fieldmeta.SetterDefinitionPrefix+tagSetter] = setterSchema(tagSetter, tag)
		// Context().Name() gives the image repository _as supplied_
		nameSetter := imageSetter + ":name"
		defs[fieldmeta.SetterDefinitionPrefix+nameSetter] = setterSchema(nameSetter, name)
	}

	// get ready with the reader and writer
	reader := &kio.LocalPackageReader{
		PackagePath:        inpath,
		IncludeSubpackages: true,
	}
	writer := &kio.LocalPackageWriter{
		PackagePath: outpath,
	}

	pipeline := kio.Pipeline{
		Inputs:  []kio.Reader{reader},
		Outputs: []kio.Writer{writer},
		Filters: []kio.Filter{kio.FilterAll(&setters2.Set{SetAll: true})},
	}

	// go!
	schemaMu.Lock()
	resetSchema()
	openapi.AddDefinitions(defs)
	err := pipeline.Execute()
	schemaMu.Unlock()
	return err
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
