/*
Copyright 2020 The Flux authors

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

package test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestFiles(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Files comparison helper")
}

var _ = Describe("when no differences", func() {
	It("matches when given the same directory", func() {
		ExpectMatchingDirectories("testdata/base", "testdata/base")
	})
	It("matches when given equivalent directories", func() {
		ExpectMatchingDirectories("testdata/base", "testdata/equiv")
	})
})

var _ = Describe("with differences", func() {
	It("finds files in expected from a/ but not in actual b/", func() {
		aonly, _, _ := DiffDirectories("testdata/diff/a", "testdata/diff/b")
		Expect(aonly).To(Equal([]string{"/only", "/only/here.yaml", "/onlyhere.yaml"}))
	})

	It("finds files in actual a/ that weren't expected from b/", func() {
		bonly, _, _ := DiffDirectories("testdata/diff/a", "testdata/diff/b") // change in order
		Expect(bonly).To(Equal([]string{"/only", "/only/here.yaml", "/onlyhere.yaml"}))
	})

	It("finds files that are different in a and b", func() {
		_, _, diffs := DiffDirectories("testdata/diff/a", "testdata/diff/b")
		var diffpaths []string
		for _, d := range diffs {
			diffpaths = append(diffpaths, d.Path())
		}
		Expect(diffpaths).To(Equal([]string{"/different/content.yaml", "/dirfile"}))
	})
})
