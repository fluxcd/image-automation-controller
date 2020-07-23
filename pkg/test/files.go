/*
Copyright 2020 Michael Bridgen <mikeb@squaremobius.net>

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
func ExpectMatchingDirectories(actualRoot, expectedRoot string) {
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
			if strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}
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
		if info.IsDir() && strings.HasPrefix(filepath.Base(p), ".") {
			return filepath.SkipDir
		}
		Expect(filepath.Join(expectedRoot, p)).To(BeAnExistingFile())
		return nil
	})
}
