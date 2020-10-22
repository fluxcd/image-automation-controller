package update

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fluxcd/image-automation-controller/pkg/test"
	imagev1alpha1_reflect "github.com/fluxcd/image-reflector-controller/api/v1alpha1"
)

func TestUpdate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Update suite")
}

var _ = Describe("Update image via kyaml setters2", func() {
	It("updates the image marked with the image policy (setter) ref", func() {
		tmp, err := ioutil.TempDir("", "gotest")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tmp)

		policies := []imagev1alpha1_reflect.ImagePolicy{
			imagev1alpha1_reflect.ImagePolicy{
				ObjectMeta: metav1.ObjectMeta{ // name matches marker used in testdata/setters/{original,expected}
					Namespace: "automation-ns",
					Name:      "policy",
				},
				Status: imagev1alpha1_reflect.ImagePolicyStatus{
					LatestImage: "updated:v1.0.1",
				},
			},
		}

		Expect(UpdateWithSetters("testdata/setters/original", tmp, policies)).To(Succeed())
		test.ExpectMatchingDirectories(tmp, "testdata/setters/expected")
	})
})
