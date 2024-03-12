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
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
)

var _ = Describe("load YAMLs with ScreeningLocalReader", func() {
	It("loads only the YAMLs containing the token", func() {
		r := ScreeningLocalReader{
			Path:  "testdata/setters/original",
			Token: "$imagepolicy",
			Trace: logr.Discard(),
		}
		nodes, err := r.Read()
		Expect(err).ToNot(HaveOccurred())
		// the test fixture has three files that contain the marker:
		// - otherns.yaml
		// - marked.yaml
		// - kustomization.yaml
		Expect(len(nodes)).To(Equal(3))
		filesSeen := map[string]struct{}{}
		for i := range nodes {
			path, _, err := kioutil.GetFileAnnotations(nodes[i])
			Expect(err).ToNot(HaveOccurred())
			filesSeen[path] = struct{}{}
		}
		Expect(filesSeen).To(Equal(map[string]struct{}{
			"marked.yaml":        struct{}{},
			"kustomization.yaml": struct{}{},
			"otherns.yaml":       struct{}{},
		}))
	})
})
