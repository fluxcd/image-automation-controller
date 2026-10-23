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
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"

	"github.com/fluxcd/image-automation-controller/internal/testutil"
)

func TestUpdateWithSetters(t *testing.T) {
	g := NewWithT(t)

	policies := []reflectorv1.ImagePolicy{
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "automation-ns",
				Name:      "policy",
			},
			Status: reflectorv1.ImagePolicyStatus{
				LatestRef: testutil.ImageToRef("index.repo.fake/updated:v1.0.1"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "automation-ns",
				Name:      "unchanged",
			},
			Status: reflectorv1.ImagePolicyStatus{
				LatestRef: testutil.ImageToRef("image:v1.0.0"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "automation-ns",
				Name:      "policy-with-digest",
			},
			Status: reflectorv1.ImagePolicyStatus{
				LatestRef: testutil.ImageToRef("image:v1.0.0@sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be"),
			},
		},
	}

	// Test Result.
	tmp := t.TempDir()
	result, err := UpdateWithSetters(logr.Discard(), "testdata/setters/original", tmp, policies)
	g.Expect(err).ToNot(HaveOccurred())
	testutil.ExpectMatchingDirectories(g, tmp, "testdata/setters/expected")

	kustomizeResourceID := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{
			APIVersion: "kustomize.config.k8s.io/v1beta1",
			Kind:       "Kustomization",
		},
	}}
	markedResourceID := ObjectIdentifier{yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{
			APIVersion: "batch/v1beta1",
			Kind:       "CronJob",
		},
		NameMeta: yaml.NameMeta{
			Namespace: "bar",
			Name:      "foo",
		},
	}}

	expectedResult := Result{
		images: map[string]reflectorv1.ImageRef{
			"automation-ns:policy":                    *policies[0].Status.LatestRef,
			"automation-ns:policy:name":               *policies[0].Status.LatestRef,
			"automation-ns:policy:tag":                *policies[0].Status.LatestRef,
			"automation-ns:policy-with-digest":        *policies[2].Status.LatestRef,
			"automation-ns:policy-with-digest:digest": *policies[2].Status.LatestRef,
		},
		FileChanges: map[string]ObjectChanges{
			"kustomization.yml": {
				kustomizeResourceID: []Change{
					{
						OldValue: "replaced",
						NewValue: "index.repo.fake/updated",
						Setter:   "automation-ns:policy:name",
					},
					{
						OldValue: "v1",
						NewValue: "v1.0.1",
						Setter:   "automation-ns:policy:tag",
					},
					{
						OldValue: "sha256:1234567890abcdef",
						NewValue: "sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be",
						Setter:   "automation-ns:policy-with-digest:digest",
					},
					{
						OldValue: "image",
						NewValue: "image:v1.0.0@sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be",
						Setter:   "automation-ns:policy-with-digest",
					},
				},
			},
			"Kustomization": {
				kustomizeResourceID: []Change{
					{
						OldValue: "replaced",
						NewValue: "index.repo.fake/updated",
						Setter:   "automation-ns:policy:name",
					},
					{
						OldValue: "v1",
						NewValue: "v1.0.1",
						Setter:   "automation-ns:policy:tag",
					},
				},
			},
			"marked.yaml": {
				markedResourceID: []Change{
					{
						OldValue: "image:v1.0.0",
						NewValue: "index.repo.fake/updated:v1.0.1",
						Setter:   "automation-ns:policy",
					},
				},
			},
		},
	}

	g.Expect(result).To(Equal(expectedResult))
	g.Expect(result.Summary()).To(Equal(
		"image -> image:v1.0.0@sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be\n" +
			"image:v1.0.0 -> index.repo.fake/updated:v1.0.1\n" +
			"image:v1.0.0 digest sha256:1234567890abcdef -> sha256:6745aaad46d795c9836632e1fb62f24b7e7f4c843144da8e47a5465c411a14be\n" +
			"index.repo.fake/updated name replaced -> index.repo.fake/updated\n" +
			"index.repo.fake/updated tag v1 -> v1.0.1"))
}

func TestUpdateWithSettersSummary(t *testing.T) {
	const image = "ghcr.io/stefanprodan/podinfo"
	const oldDigest = "sha256:1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b"
	const newDigest = "sha256:7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b"
	tests := []struct {
		fixture string
		ref     reflectorv1.ImageRef
		setters []string
		want    string
	}{
		{
			fixture: "full-reference",
			ref:     reflectorv1.ImageRef{Name: image, Tag: "latest", Digest: newDigest},
			setters: []string{"automation-ns:policy"},
			want:    "podinfo:latest@" + oldDigest + " -> " + image + ":latest@" + newDigest,
		},
		{
			fixture: "components",
			ref:     reflectorv1.ImageRef{Name: image, Tag: "6.5.1", Digest: newDigest},
			setters: []string{"automation-ns:policy:tag", "automation-ns:policy:digest"},
			want: image + " tag 6.5.0 -> 6.5.1\n" +
				image + ":6.5.1 digest " + oldDigest + " -> " + newDigest,
		},
		{
			fixture: "latest",
			ref:     reflectorv1.ImageRef{Name: image, Tag: "latest", Digest: newDigest},
			setters: []string{"automation-ns:policy:digest"},
			want:    image + ":latest digest " + oldDigest + " -> " + newDigest,
		},
		{
			fixture: "unmanaged-tag",
			ref:     reflectorv1.ImageRef{Name: image, Tag: "latest", Digest: newDigest},
			setters: []string{"automation-ns:policy:digest"},
			want:    image + ":latest digest " + oldDigest + " -> " + newDigest,
		},
		{
			fixture: "name",
			ref:     reflectorv1.ImageRef{Name: "podinfo", Tag: "latest"},
			setters: []string{"automation-ns:policy:name"},
			want:    "podinfo name docker.io/library/podinfo -> podinfo",
		},
		{
			fixture: "numeric-tag",
			ref:     reflectorv1.ImageRef{Name: "podinfo", Tag: "001"},
			setters: []string{"automation-ns:policy:tag"},
			want:    "podinfo tag 000 -> 001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			g := NewWithT(t)
			ref := tt.ref
			policies := []reflectorv1.ImagePolicy{{
				ObjectMeta: metav1.ObjectMeta{Namespace: "automation-ns", Name: "policy"},
				Status:     reflectorv1.ImagePolicyStatus{LatestRef: &ref},
			}}
			path := filepath.Join("testdata/summary", tt.fixture)
			out := t.TempDir()
			result, err := UpdateWithSetters(logr.Discard(), filepath.Join(path, "original"), out, policies)
			g.Expect(err).ToNot(HaveOccurred())
			testutil.ExpectMatchingDirectories(g, out, filepath.Join(path, "expected"))
			g.Expect(result.Summary()).To(Equal(tt.want))
			g.Expect(result.Changes()).To(HaveLen(len(tt.setters)))

			expectedImages := make(map[string]reflectorv1.ImageRef)
			for _, setter := range tt.setters {
				expectedImages[setter] = tt.ref
			}
			g.Expect(result.images).To(Equal(expectedImages))

			unchanged, err := UpdateWithSetters(logr.Discard(), out, t.TempDir(), policies)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(unchanged.images).To(BeNil())
			g.Expect(unchanged.Summary()).To(BeEmpty())

			*policies[0].Status.LatestRef = reflectorv1.ImageRef{Name: "other", Tag: "other"}
			g.Expect(result.images).To(Equal(expectedImages))
			g.Expect(result.Summary()).To(Equal(tt.want))
		})
	}
}
