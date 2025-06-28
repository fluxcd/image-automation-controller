package update

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// mustRef creates an imageRef for use in tests. It panics if the ref
// given is invalid.
func mustRef(ref string) imageRef {
	r, err := name.ParseReference(ref)
	if err != nil {
		panic(err)
	}
	return imageRef{r, types.NamespacedName{}}
}

func TestMustRef(t *testing.T) {
	g := NewWithT(t)

	t.Run("gives each component of an image ref", func(t *testing.T) {
		ref := mustRef("helloworld:v1.0.1")
		g.Expect(ref.String()).To(Equal("helloworld:v1.0.1"))
		g.Expect(ref.Identifier()).To(Equal("v1.0.1"))
		g.Expect(ref.Repository()).To(Equal("library/helloworld"))
		g.Expect(ref.Registry()).To(Equal("index.docker.io"))
		g.Expect(ref.Name()).To(Equal("index.docker.io/library/helloworld:v1.0.1"))
	})

	t.Run("deals with hostnames and digests", func(t *testing.T) {
		image := "localhost:5000/org/helloworld@sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be"
		ref := mustRef(image)
		g.Expect(ref.String()).To(Equal(image))
		g.Expect(ref.Identifier()).To(Equal("sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be"))
		g.Expect(ref.Repository()).To(Equal("org/helloworld"))
		g.Expect(ref.Registry()).To(Equal("localhost:5000"))
		g.Expect(ref.Name()).To(Equal(image))
	})
}

func TestResult(t *testing.T) {
	g := NewWithT(t)

	var result Result
	objectNames := []ObjectIdentifier{
		{yaml.ResourceIdentifier{
			NameMeta: yaml.NameMeta{Namespace: "ns", Name: "foo"},
		}},
		{yaml.ResourceIdentifier{
			NameMeta: yaml.NameMeta{Namespace: "ns", Name: "bar"},
		}},
	}

	result.AddChange("foo.yaml", objectNames[0], Change{
		OldValue: "aaa",
		NewValue: "bbb",
		Setter:   "foo-ns:policy:name",
	})
	result.AddChange("bar.yaml", objectNames[1], Change{
		OldValue: "cccc:v1.0",
		NewValue: "cccc:v1.2",
		Setter:   "foo-ns:policy",
	})

	result = Result{
		FileChanges: map[string]ObjectChanges{
			"foo.yaml": {
				objectNames[0]: []Change{
					{
						OldValue: "aaa",
						NewValue: "bbb",
						Setter:   "foo-ns:policy:name",
					},
				},
			},
			"bar.yaml": {
				objectNames[1]: []Change{
					{
						OldValue: "cccc:v1.0",
						NewValue: "cccc:v1.2",
						Setter:   "foo-ns:policy",
					},
				},
			},
		},
	}

	g.Expect(result.Changes()).To(ContainElements([]Change{
		{
			OldValue: "aaa",
			NewValue: "bbb",
			Setter:   "foo-ns:policy:name",
		},
		{
			OldValue: "cccc:v1.0",
			NewValue: "cccc:v1.2",
			Setter:   "foo-ns:policy",
		},
	}))
	g.Expect(result.Objects()).To(Equal(ObjectChanges{
		objectNames[0]: []Change{
			{
				OldValue: "aaa",
				NewValue: "bbb",
				Setter:   "foo-ns:policy:name",
			},
		},
		objectNames[1]: []Change{
			{
				OldValue: "cccc:v1.0",
				NewValue: "cccc:v1.2",
				Setter:   "foo-ns:policy",
			},
		},
	}))
}
