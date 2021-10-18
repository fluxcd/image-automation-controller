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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/gomega"
)

// TODO rewrite this as just doing the diff, so I can test that it
// fails at the right times too.
func ExpectMatchingDirectories(g *WithT, actualRoot, expectedRoot string) {
	g.Expect(actualRoot).To(BeADirectory())
	g.Expect(expectedRoot).To(BeADirectory())
	actualonly, expectedonly, different := DiffDirectories(actualRoot, expectedRoot)
	g.Expect(actualonly).To(BeEmpty(), "Expect no files in %s but not in %s", actualRoot, expectedRoot)
	g.Expect(expectedonly).To(BeEmpty(), "Expect no files in %s but not in %s", expectedRoot, actualRoot)
	// these are enumerated, so that the output is the actual difference
	for _, diff := range different {
		diff.FailedExpectation(g)
	}
}

type Diff interface {
	Path() string
	FailedExpectation(g *WithT)
}

type contentdiff struct {
	path, actual, expected string
}

func (d contentdiff) Path() string {
	return d.path
}

// Run an expectation that will fail, giving an appropriate error
func (d contentdiff) FailedExpectation(g *WithT) {
	g.Expect(d.actual).To(Equal(d.expected))
}

type dirfile struct {
	abspath, path       string
	expectedRegularFile bool
}

func (d dirfile) Path() string {
	return d.path
}

func (d dirfile) FailedExpectation(g *WithT) {
	if d.expectedRegularFile {
		g.Expect(d.path).To(BeARegularFile())
	} else {
		g.Expect(d.path).To(BeADirectory())
	}
}

// DiffDirectories walks the two given directories, recursively, and
// reports relative paths for any files that are:
//
//     (in actual but not expected, in expected but not actual, in both but different)
//
// It ignores dot directories (e.g., `.git/`) and Emacs backups (e.g.,
// `foo.yaml~`). It panics if it encounters any error apart from a
// file not found.
func DiffDirectories(actual, expected string) (actualonly []string, expectedonly []string, different []Diff) {
	seen := make(map[string]struct{})

	filepath.Walk(expected, func(expectedPath string, expectedInfo os.FileInfo, err error) error {
		if err != nil {
			panic(err)
		}

		relPath := expectedPath[len(expected):]
		seen[relPath] = struct{}{}

		// ignore emacs backups
		if strings.HasSuffix(expectedPath, "~") {
			return nil
		}

		// ignore dotfiles
		if strings.HasPrefix(filepath.Base(expectedPath), ".") {
			if expectedInfo.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		actualPath := filepath.Join(actual, relPath)
		actualInfo, err := os.Stat(actualPath)
		switch {
		case err == nil:
			break
		case os.IsNotExist(err):
			expectedonly = append(expectedonly, relPath)
			if expectedInfo.IsDir() {
				return filepath.SkipDir
			}
			return nil
		default:
			panic(err)
		}

		// file exists in both places
		switch {
		case actualInfo.IsDir() && expectedInfo.IsDir():
			return nil // i.e., keep recursing
		case actualInfo.IsDir() || expectedInfo.IsDir():
			different = append(different, dirfile{path: relPath, abspath: actualPath, expectedRegularFile: actualInfo.IsDir()})
			if expectedInfo.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// both regular files

		actualBytes, err := ioutil.ReadFile(actualPath)
		if err != nil {
			panic(err)
		}
		expectedBytes, err := ioutil.ReadFile(expectedPath)
		if err != nil {
			panic(err)
		}
		if string(actualBytes) != string(expectedBytes) {
			different = append(different, contentdiff{path: relPath, actual: string(actualBytes), expected: string(expectedBytes)})
		}
		return nil
	})

	// every file and directory in the actual result should be expected
	filepath.Walk(actual, func(actualPath string, actualInfo os.FileInfo, err error) error {
		if err != nil {
			panic(err)
		}
		relPath := actualPath[len(actual):]
		// ignore emacs backups
		if strings.HasSuffix(actualPath, "~") {
			return nil
		}
		// skip dotdirs
		if actualInfo.IsDir() && strings.HasPrefix(filepath.Base(actualPath), ".") {
			return filepath.SkipDir
		}

		if _, ok := seen[relPath]; !ok {
			actualonly = append(actualonly, relPath)
			if actualInfo.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	return
}
