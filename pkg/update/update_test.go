package update

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/image-automation-controller/pkg/test"
)

func TestUpdate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Update suite")
}

var _ = Describe("Update image everywhere", func() {
	It("leaves a different image alone", func() {
		tmp, err := ioutil.TempDir("", "gotest")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tmp)
		Expect(UpdateImageEverywhere("testdata/leave/original", tmp, "notused", "notused:v1.0.1")).To(Succeed())
		test.ExpectMatchingDirectories("testdata/leave/expected", tmp)
	})

	It("replaces the given image", func() {
		tmp, err := ioutil.TempDir("", "gotest")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tmp)
		Expect(UpdateImageEverywhere("testdata/replace/original", tmp, "used", "used:v1.1.0")).To(Succeed())
		test.ExpectMatchingDirectories("testdata/replace/expected", tmp)
	})

	It("keeps comments intact", func() {
		tmp, err := ioutil.TempDir("", "gotest")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tmp)
		Expect(UpdateImageEverywhere("testdata/replace/commented", tmp, "used", "used:v1.1.0")).To(Succeed())
		test.ExpectMatchingDirectories("testdata/replace/commented-expected", tmp)
	})
})
