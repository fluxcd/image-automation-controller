package update

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// TODO rewrite this as just doing the diff, so I can test that it
// fails at the right times too.
func expectMatchingDirectories(actualRoot, expectedRoot string) {
	Expect(actualRoot).To(BeADirectory())
	filepath.Walk(expectedRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// ignore emacs backups
		if strings.HasSuffix(path, "~") {
			return nil
		}
		relPath := path[len(expectedRoot):]
		actualPath := filepath.Join(actualRoot, relPath)
		if info.IsDir() {
			Expect(actualPath).To(BeADirectory())
			return nil
		}
		Expect(actualPath).To(BeARegularFile())
		actualBytes, err := ioutil.ReadFile(actualPath)
		expectedBytes, err := ioutil.ReadFile(path)
		Expect(string(actualBytes)).To(Equal(string(expectedBytes)))
		return nil
	})
	filepath.Walk(actualRoot, func(path string, info os.FileInfo, err error) error {
		p := path[len(actualRoot):]
		// ignore emacs backups
		if strings.HasSuffix(p, "~") {
			return nil
		}
		Expect(filepath.Join(expectedRoot, p)).To(BeAnExistingFile())
		return nil
	})
}

func TestUpdate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Update suite")
}

var _ = Describe("Test helper", func() {
	It("matches when given the same directory", func() {
		expectMatchingDirectories("testdata/base", "testdata/base")
	})
	It("matches when given equivalent directories", func() {
		expectMatchingDirectories("testdata/base", "testdata/equiv")
	})
})

var _ = Describe("Update image everywhere", func() {
	It("leaves a different image alone", func() {
		tmp, err := ioutil.TempDir("", "gotest")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tmp)
		Expect(UpdateImageEverywhere("testdata/leave/original", tmp, "notused", "notused:v1.0.1")).To(Succeed())
		expectMatchingDirectories("testdata/leave/expected", tmp)
	})

	It("replaces the given image", func() {
		tmp, err := ioutil.TempDir("", "gotest")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tmp)
		Expect(UpdateImageEverywhere("testdata/replace/original", tmp, "used", "used:v1.1.0")).To(Succeed())
		expectMatchingDirectories("testdata/replace/expected", tmp)
	})
})
