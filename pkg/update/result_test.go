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

func TestUpdateResults(t *testing.T) {
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

	g.Expect(result.Images()).To(Equal([]ImageRef{
		mustRef("image:v1.0"),
		mustRef("other:v2.0"),
	}))

	g.Expect(result.Objects()).To(Equal(map[ObjectIdentifier][]ImageRef{
		objectNames[0]: {
			mustRef("image:v1.0"),
			mustRef("other:v2.0"),
		},
		objectNames[1]: {
			mustRef("image:v1.0"),
			mustRef("other:v2.0"),
		},
	}))
}

func TestResultV2(t *testing.T) {
	g := NewWithT(t)

	var result ResultV2
	objectNames := []ObjectIdentifier{
		{
			yaml.ResourceIdentifier{
				NameMeta: yaml.NameMeta{Namespace: "ns", Name: "foo"},
			},
		},
		{
			yaml.ResourceIdentifier{
				NameMeta: yaml.NameMeta{Namespace: "ns", Name: "bar"},
			},
		},
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

	result = ResultV2{
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

	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "TestResultV2_AddChange",
			test: func(t *testing.T) {
				g := NewWithT(t)

				var result ResultV2
				objectName := ObjectIdentifier{
					yaml.ResourceIdentifier{
						NameMeta: yaml.NameMeta{Namespace: "ns", Name: "test"},
					},
				}

				result.AddChange("test.yaml", objectName, Change{
					OldValue: "old",
					NewValue: "new",
					Setter:   "test-ns:policy",
				})

				expectedChanges := ObjectChanges{
					objectName: []Change{
						{
							OldValue: "old",
							NewValue: "new",
							Setter:   "test-ns:policy",
						},
					},
				}

				// Check if the change was added correctly
				g.Expect(result.FileChanges["test.yaml"]).To(Equal(expectedChanges))
			},
		},
		{
			name: "TestResultV2_Changes",
			test: func(t *testing.T) {
				g := NewWithT(t)

				var result ResultV2
				objectName := ObjectIdentifier{
					yaml.ResourceIdentifier{
						NameMeta: yaml.NameMeta{Namespace: "ns", Name: "test"},
					},
				}

				result.FileChanges = map[string]ObjectChanges{
					"test.yaml": {
						objectName: []Change{
							{
								OldValue: "old",
								NewValue: "new",
								Setter:   "test-ns:policy",
							},
						},
					},
				}

				// Check if the changes are returned correctly
				g.Expect(result.Changes()).To(Equal([]Change{
					{
						OldValue: "old",
						NewValue: "new",
						Setter:   "test-ns:policy",
					},
				}))
			},
		},
		{
			name: "TestResultV2_Objects",
			test: func(t *testing.T) {
				g := NewWithT(t)

				var result ResultV2
				objectName := ObjectIdentifier{
					yaml.ResourceIdentifier{
						NameMeta: yaml.NameMeta{Namespace: "ns", Name: "test"},
					},
				}

				result.FileChanges = map[string]ObjectChanges{
					"test.yaml": {
						objectName: []Change{
							{
								OldValue: "old",
								NewValue: "new",
								Setter:   "test-ns:policy",
							},
						},
					},
				}

				// Check if the objects are returned correctly
				g.Expect(result.Objects()).To(Equal(ObjectChanges{
					objectName: []Change{
						{
							OldValue: "old",
							NewValue: "new",
							Setter:   "test-ns:policy",
						},
					},
				}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}
