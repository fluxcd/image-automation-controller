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
	"io/ioutil"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/name"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/fluxcd/image-automation-controller/pkg/test"
	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta1"
)

func TestUpdateWithSetters(t *testing.T) {
	g := NewWithT(t)

	policies := []imagev1_reflect.ImagePolicy{
		{
			ObjectMeta: metav1.ObjectMeta{ // name matches marker used in testdata/setters/{original,expected}
				Namespace: "automation-ns",
				Name:      "policy",
			},
			Status: imagev1_reflect.ImagePolicyStatus{
				LatestImage: "index.repo.fake/updated:v1.0.1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{ // name matches marker used in testdata/setters/{original,expected}
				Namespace: "automation-ns",
				Name:      "unchanged",
			},
			Status: imagev1_reflect.ImagePolicyStatus{
				LatestImage: "image:v1.0.0",
			},
		},
	}

	tmp, err := ioutil.TempDir("", "gotest")
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tmp)

	result, err := UpdateWithSetters(logr.Discard(), "testdata/setters/original", tmp, policies)
	g.Expect(err).ToNot(HaveOccurred())
	test.ExpectMatchingDirectories(g, tmp, "testdata/setters/expected")

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
	r, _ := name.ParseReference("index.repo.fake/updated:v1.0.1")
	expectedImageRef := imageRef{r, types.NamespacedName{
		Name:      "policy",
		Namespace: "automation-ns",
	}}

	expectedResult := Result{
		Files: map[string]FileResult{
			"kustomization.yaml": {
				Objects: map[ObjectIdentifier][]ImageRef{
					kustomizeResourceID: {
						expectedImageRef,
					},
				},
			},
			"marked.yaml": {
				Objects: map[ObjectIdentifier][]ImageRef{
					markedResourceID: {
						expectedImageRef,
					},
				},
			},
		},
	}

	g.Expect(result).To(Equal(expectedResult))
}
