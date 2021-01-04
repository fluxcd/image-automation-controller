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

// UpdateWithSetters takes all YAML files from `inpath`, updates any
// that contain an "in scope" image policy marker, and writes files it
// updated (and only those files) back to `outpath`.
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
			setters2.SetAll( // run the enclosed single-node setters2.Filter on all nodes,
				// and only include those in files that changed in the output
				&setters2.Set{SetAll: true}, // set all images that are in the constructed schema
			),
		},
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
