package update

import (
	"github.com/google/go-containerregistry/pkg/name"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func mustRef(ref string) name.Reference {
	r, err := name.ParseReference(ref)
	if err != nil {
		panic(err)
	}
	return r
}

var _ = Describe("update results", func() {

	var result Result
	objectNames := []yaml.ResourceIdentifier{
		yaml.ResourceIdentifier{
			NameMeta: yaml.NameMeta{Namespace: "ns", Name: "foo"},
		},
		yaml.ResourceIdentifier{
			NameMeta: yaml.NameMeta{Namespace: "ns", Name: "bar"},
		},
	}

	BeforeEach(func() {
		result = Result{
			Files: map[string]FileResult{
				"foo.yaml": {
					Objects: map[yaml.ResourceIdentifier][]name.Reference{
						objectNames[0]: {
							mustRef("image:v1.0"),
							mustRef("other:v2.0"),
						},
					},
				},
				"bar.yaml": {
					Objects: map[yaml.ResourceIdentifier][]name.Reference{
						objectNames[1]: {
							mustRef("image:v1.0"),
							mustRef("other:v2.0"),
						},
					},
				},
			},
		}
	})

	It("deduplicates images", func() {
		Expect(result.Images()).To(Equal([]name.Reference{
			mustRef("image:v1.0"),
			mustRef("other:v2.0"),
		}))
	})

	It("collects images by object", func() {
		Expect(result.Objects()).To(Equal(map[yaml.ResourceIdentifier][]name.Reference{
			objectNames[0]: {
				mustRef("image:v1.0"),
				mustRef("other:v2.0"),
			},
			objectNames[1]: {
				mustRef("image:v1.0"),
				mustRef("other:v2.0"),
			},
		}))
	})
})
