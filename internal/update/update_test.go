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

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"

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
}
