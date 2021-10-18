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

	. "github.com/onsi/gomega"
)

func TestExpectMatchingDirectories(t *testing.T) {
	tests := []struct {
		name         string
		actualRoot   string
		expectedRoot string
	}{
		{
			name:         "same directory",
			actualRoot:   "testdata/base",
			expectedRoot: "testdata/base",
		},
		{
			name:         "different equivalent directories",
			actualRoot:   "testdata/base",
			expectedRoot: "testdata/equiv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ExpectMatchingDirectories(g, tt.actualRoot, tt.expectedRoot)
		})
	}
}

func TestDiffDirectories(t *testing.T) {
	g := NewWithT(t)

	// Finds files in actual a/ that weren't expected from b/.
	actualonly, _, _ := DiffDirectories("testdata/diff/a", "testdata/diff/b")
	g.Expect(actualonly).To(Equal([]string{"/only", "/onlyhere.yaml"}))

	// Finds files in expected from a/ but not in actual b/.
	_, expectedonly, _ := DiffDirectories("testdata/diff/b", "testdata/diff/a") // NB change in order
	g.Expect(expectedonly).To(Equal([]string{"/only", "/onlyhere.yaml"}))

	// Finds files that are different in a and b.
	_, _, diffs := DiffDirectories("testdata/diff/a", "testdata/diff/b")
	var diffpaths []string
	for _, d := range diffs {
		diffpaths = append(diffpaths, d.Path())
	}
	g.Expect(diffpaths).To(Equal([]string{"/different/content.yaml", "/dirfile"}))
}
