package update

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
)

var _ = Describe("load YAMLs with ScreeningLocalReader", func() {
	It("loads only the YAMLs containing the token", func() {
		r := ScreeningLocalReader{
			Path:  "testdata/setters/original",
			Token: "$imagepolicy",
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
