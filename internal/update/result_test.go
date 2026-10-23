/*
Copyright 2025 The Flux authors

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
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"
)

// mustRef creates an imageRef for use in tests. It panics if the ref
// given is invalid.
func mustRef(ref string) imageRef {
	r, err := name.ParseReference(ref)
	if err != nil {
		panic(err)
	}
	return imageRef{Reference: r, policy: types.NamespacedName{}}
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

func TestResultObjectsAggregatesSameObjectAcrossFiles(t *testing.T) {
	g := NewWithT(t)

	objectID := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		NameMeta: yaml.NameMeta{
			Namespace: "default",
			Name:      "podinfo",
		},
	}}

	result := Result{
		FileChanges: map[string]ObjectChanges{
			"base/deployment.yaml": {
				objectID: []Change{
					{
						OldValue: "podinfo:v1.0.0",
						NewValue: "podinfo:v1.0.1",
						Setter:   "flux-system:podinfo",
					},
				},
			},
			"overlays/prod/deployment.yaml": {
				objectID: []Change{
					{
						OldValue: "1",
						NewValue: "2",
						Setter:   "flux-system:replicas",
					},
				},
			},
		},
	}

	objects := result.Objects()
	g.Expect(objects).To(HaveKey(objectID))
	g.Expect(objects[objectID]).To(ContainElements(
		Change{
			OldValue: "podinfo:v1.0.0",
			NewValue: "podinfo:v1.0.1",
			Setter:   "flux-system:podinfo",
		},
		Change{
			OldValue: "1",
			NewValue: "2",
			Setter:   "flux-system:replicas",
		},
	))
}

func TestResultIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	deployment := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		NameMeta: yaml.NameMeta{Namespace: "default", Name: "podinfo"},
	}}
	cronJob := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{APIVersion: "batch/v1", Kind: "CronJob"},
		NameMeta: yaml.NameMeta{Namespace: "default", Name: "cleanup"},
	}}

	image := Change{OldValue: "podinfo:v1.0.0", NewValue: "podinfo:v1.0.1", Setter: "flux-system:podinfo"}
	replicas := Change{OldValue: "1", NewValue: "2", Setter: "flux-system:replicas"}
	tag := Change{OldValue: "2", NewValue: "3", Setter: "flux-system:podinfo:tag"}
	cleanup := Change{OldValue: "cleanup:v1.0.0", NewValue: "cleanup:v1.0.1", Setter: "flux-system:cleanup"}

	result := Result{
		FileChanges: map[string]ObjectChanges{
			"b.yaml": {
				deployment: {tag},
			},
			"a.yaml": {
				cronJob:    {cleanup},
				deployment: {image, replicas},
			},
		},
	}

	for range 50 {
		g.Expect(result.Changes()).To(Equal([]Change{image, replicas, cleanup, tag}))
		g.Expect(result.Objects()).To(Equal(ObjectChanges{
			deployment: {image, replicas, tag},
			cronJob:    {cleanup},
		}))
	}
}

func TestResultSummary(t *testing.T) {
	const image = "ghcr.io/stefanprodan/podinfo"
	const oldDigest = "sha256:1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b"
	const newDigest = "sha256:7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b"
	object := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		NameMeta: yaml.NameMeta{Namespace: "default", Name: "podinfo"},
	}}
	otherObject := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		NameMeta: yaml.NameMeta{Namespace: "default", Name: "other"},
	}}
	kustomization := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{APIVersion: "kustomize.config.k8s.io/v1beta1", Kind: "Kustomization"},
	}}
	tag := Change{OldValue: "6.5.0", NewValue: "6.5.1", Setter: "ns:policy:tag"}
	olderTag := Change{OldValue: "6.4.0", NewValue: "6.5.1", Setter: "ns:policy:tag"}
	digest := Change{OldValue: oldDigest, NewValue: newDigest, Setter: "ns:policy:digest"}
	images := map[string]reflectorv1.ImageRef{
		"ns:policy":        {Name: image, Tag: "6.5.1", Digest: newDigest},
		"ns:policy:tag":    {Name: image, Tag: "6.5.1", Digest: newDigest},
		"ns:policy:digest": {Name: image, Tag: "6.5.1", Digest: newDigest},
		"ns:policy:name":   {Name: image, Tag: "6.5.1", Digest: newDigest},
	}

	tests := []struct {
		name   string
		result Result
		want   string
	}{
		{
			name: "full reference including name change",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: "image:v1.0.0", NewValue: "index.repo.fake/updated:v1.0.1", Setter: "ns:policy"},
				}}},
				images: images,
			},
			want: "image:v1.0.0 -> index.repo.fake/updated:v1.0.1",
		},
		{
			name: "single tag change without prefix or trailing newline",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {tag}}},
				images:      images,
			},
			want: image + " tag 6.5.0 -> 6.5.1",
		},
		{
			name: "digest labelled with policy tag",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {digest}}},
				images: map[string]reflectorv1.ImageRef{
					"ns:policy:digest": {Name: image, Tag: "latest", Digest: newDigest},
				},
			},
			want: image + ":latest digest " + oldDigest + " -> " + newDigest,
		},
		{
			name: "digest without policy tag",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {digest}}},
				images: map[string]reflectorv1.ImageRef{
					"ns:policy:digest": {Name: image, Digest: newDigest},
				},
			},
			want: image + " digest " + oldDigest + " -> " + newDigest,
		},
		{
			name: "name change",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: "docker.io/stefanprodan/podinfo", NewValue: image, Setter: "ns:policy:name"},
				}}},
				images: images,
			},
			want: image + " name docker.io/stefanprodan/podinfo -> " + image,
		},
		{
			name: "tag and digest in one object sorted with new tag label",
			result: Result{
				FileChanges: map[string]ObjectChanges{"kustomization.yaml": {kustomization: {digest, tag}}},
				images:      images,
			},
			want: image + " tag 6.5.0 -> 6.5.1\n" + image + ":6.5.1 digest " + oldDigest + " -> " + newDigest,
		},
		{
			name: "spelling-only full reference name change",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: "app:1", NewValue: "docker.io/library/app:1", Setter: "ns:policy"},
				}}},
				images: map[string]reflectorv1.ImageRef{"ns:policy": {Name: "docker.io/library/app", Tag: "1"}},
			},
			want: "app:1 -> docker.io/library/app:1",
		},
		{
			name: "unexpected setter shapes fall back",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: "a", NewValue: "b", Setter: "tag"},
					{OldValue: "c", NewValue: "d", Setter: "ns:policy:extra:tag"},
					{OldValue: "e", NewValue: "f", Setter: "ns:policy:unknown"},
					{OldValue: "g", NewValue: "h", Setter: ""},
				}}},
				images: map[string]reflectorv1.ImageRef{
					"tag":                 {Name: image},
					"ns:policy:extra:tag": {Name: image},
					"ns:policy:unknown":   {Name: image},
					"":                    {Name: image},
				},
			},
			want: "a -> b\nc -> d\ne -> f\ng -> h",
		},
		{
			name: "full reference digests are not shortened",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: image + ":latest@" + oldDigest, NewValue: image + ":latest@" + newDigest, Setter: "ns:policy"},
				}}},
				images: map[string]reflectorv1.ImageRef{"ns:policy": {Name: image, Tag: "latest", Digest: newDigest}},
			},
			want: image + ":latest@" + oldDigest + " -> " + image + ":latest@" + newDigest,
		},
		{
			name: "same change in several files and objects",
			result: Result{
				FileChanges: map[string]ObjectChanges{
					"a.yaml": {object: {tag}, otherObject: {tag}},
					"b.yaml": {object: {tag}},
				},
				images: images,
			},
			want: image + " tag 6.5.0 -> 6.5.1",
		},
		{
			name: "different old values in two files",
			result: Result{
				FileChanges: map[string]ObjectChanges{
					"a.yaml": {object: {tag}},
					"b.yaml": {object: {olderTag}},
				},
				images: images,
			},
			want: image + " tag 6.4.0 -> 6.5.1\n" + image + " tag 6.5.0 -> 6.5.1",
		},
		{
			name: "different old values twice in one object",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {tag, olderTag}}},
				images:      images,
			},
			want: image + " tag 6.4.0 -> 6.5.1\n" + image + " tag 6.5.0 -> 6.5.1",
		},
		{
			name: "two policies with identical image changes give one line",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					tag,
					{OldValue: "6.5.0", NewValue: "6.5.1", Setter: "ns:other:tag"},
				}}},
				images: map[string]reflectorv1.ImageRef{
					"ns:policy:tag": {Name: image, Tag: "6.5.1"},
					"ns:other:tag":  {Name: image, Tag: "6.5.1"},
				},
			},
			want: image + " tag 6.5.0 -> 6.5.1",
		},
		{
			name: "two policies with different updates to the same image",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					tag,
					{OldValue: "6.5.0", NewValue: "6.6.0", Setter: "ns:other:tag"},
				}}},
				images: map[string]reflectorv1.ImageRef{
					"ns:policy:tag": {Name: image, Tag: "6.5.1"},
					"ns:other:tag":  {Name: image, Tag: "6.6.0"},
				},
			},
			want: image + " tag 6.5.0 -> 6.5.1\n" + image + " tag 6.5.0 -> 6.6.0",
		},
		{
			name: "policy named tag is a full reference",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: "app:1", NewValue: "app:2", Setter: "ns:tag"},
				}}},
				images: map[string]reflectorv1.ImageRef{"ns:tag": {Name: "app", Tag: "2"}},
			},
			want: "app:1 -> app:2",
		},
		{
			name: "setter missing from images falls back",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {tag}}},
				images:      map[string]reflectorv1.ImageRef{"ns:other:tag": {Name: image, Tag: "6.5.1"}},
			},
			want: "6.5.0 -> 6.5.1",
		},
		{
			name: "nil images falls back",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {tag, digest}}},
			},
			want: "6.5.0 -> 6.5.1\n" + oldDigest + " -> " + newDigest,
		},
		{
			name: "numeric and digest-looking tags",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: "001", NewValue: "sha256-abc", Setter: "ns:policy:tag"},
				}}},
				images: map[string]reflectorv1.ImageRef{"ns:policy:tag": {Name: image, Tag: "sha256-abc"}},
			},
			want: image + " tag 001 -> sha256-abc",
		},
		{
			name: "first digest preserves empty old value",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: "", NewValue: newDigest, Setter: "ns:policy:digest"},
				}}},
				images: images,
			},
			want: image + ":6.5.1 digest  -> " + newDigest,
		},
		{
			name: "cleared digest preserves empty new value",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: oldDigest, NewValue: "", Setter: "ns:policy:digest"},
				}}},
				images: map[string]reflectorv1.ImageRef{"ns:policy:digest": {Name: image, Tag: "6.5.1"}},
			},
			want: image + ":6.5.1 digest " + oldDigest + " -> ",
		},
		{
			name: "values are not escaped quoted or trimmed",
			result: Result{
				FileChanges: map[string]ObjectChanges{"pod.yaml": {object: {
					{OldValue: " 'old' ", NewValue: "new\n\"value\"\\", Setter: "ns:policy:tag"},
				}}},
				images: images,
			},
			want: image + " tag  'old'  -> new\n\"value\"\\",
		},
		{
			name: "lines sorted by image rather than file or change order",
			result: Result{
				FileChanges: map[string]ObjectChanges{
					"a.yaml": {object: {tag}},
					"z.yaml": {otherObject: {
						{OldValue: "1", NewValue: "2", Setter: "ns:other:tag"},
					}},
				},
				images: map[string]reflectorv1.ImageRef{
					"ns:policy:tag": {Name: image, Tag: "6.5.1"},
					"ns:other:tag":  {Name: "app", Tag: "2"},
				},
			},
			want: "app tag 1 -> 2\n" + image + " tag 6.5.0 -> 6.5.1",
		},
		{
			name:   "empty result",
			result: Result{},
			want:   "",
		},
		{
			name:   "images without changes",
			result: Result{images: images},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			for range 50 {
				g.Expect(tt.result.Summary()).To(Equal(tt.want))
			}
		})
	}
}
