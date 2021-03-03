package update

import (
	"github.com/google/go-containerregistry/pkg/name"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func mustRef(ref string) imageRef {
	r, err := name.ParseReference(ref)
	if err != nil {
		panic(err)
	}
	return imageRef{r}
}

var _ = Describe("image ref", func() {
	It("gives each component of an image ref", func() {
		ref := mustRef("helloworld:v1.0.1")
		Expect(ref.String()).To(Equal("helloworld:v1.0.1"))
		Expect(ref.Identifier()).To(Equal("v1.0.1"))
		Expect(ref.Repository()).To(Equal("library/helloworld"))
		Expect(ref.Registry()).To(Equal("index.docker.io"))
		Expect(ref.Name()).To(Equal("index.docker.io/library/helloworld:v1.0.1"))
	})

	It("deals with hostnames and digests", func() {
		image := "localhost:5000/org/helloworld@sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be"
		ref := mustRef(image)
		Expect(ref.String()).To(Equal(image))
		Expect(ref.Identifier()).To(Equal("sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be"))
		Expect(ref.Repository()).To(Equal("org/helloworld"))
		Expect(ref.Registry()).To(Equal("localhost:5000"))
		Expect(ref.Name()).To(Equal(image))
	})
})

var _ = Describe("update results", func() {

	var result Result
	objectNames := []ObjectIdentifier{
		ObjectIdentifier{yaml.ResourceIdentifier{
			NameMeta: yaml.NameMeta{Namespace: "ns", Name: "foo"},
		}},
		ObjectIdentifier{yaml.ResourceIdentifier{
			NameMeta: yaml.NameMeta{Namespace: "ns", Name: "bar"},
		}},
	}

	BeforeEach(func() {
		result = Result{
			Files: map[string]FileResult{
				"foo.yaml": {
					Objects: map[ObjectIdentifier][]ImageRef{
						objectNames[0]: {
							mustRef("image:v1.0"),
							mustRef("other:v2.0"),
						},
					},
				},
				"bar.yaml": {
					Objects: map[ObjectIdentifier][]ImageRef{
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
		Expect(result.Images()).To(Equal([]ImageRef{
			mustRef("image:v1.0"),
			mustRef("other:v2.0"),
		}))
	})

	It("collects images by object", func() {
		Expect(result.Objects()).To(Equal(map[ObjectIdentifier][]ImageRef{
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
