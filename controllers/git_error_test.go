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

package controllers

import (
	"errors"
	"testing"
)

func TestLibgit2ErrorTidy(t *testing.T) {
	// this is what GitLab sends if the deploy key doesn't have write access
	gitlabMessage := `remote: 
remote: ========================================================================
remote: 
remote: This deploy key does not have write access to this project.
remote: 
remote: ========================================================================
remote: 
`
	expectedReformat := "remote: This deploy key does not have write access to this project."

	err := errors.New(gitlabMessage)
	err = libgit2PushError(err)
	reformattedMessage := err.Error()
	if reformattedMessage != expectedReformat {
		t.Errorf("expected %q, got %q", expectedReformat, reformattedMessage)
	}
}

func TestLibgit2Multiline(t *testing.T) {
	// this is a hypothetical error message, in which the useful
	// content spans more than one line
	multilineMessage := `remote: 
remote: ========================================================================
remote: 
remote: This deploy key does not have write access to this project.
remote: You will need to create a new deploy key.
remote: 
remote: ========================================================================
remote: 
`
	expectedReformat := "remote: This deploy key does not have write access to this project. You will need to create a new deploy key."

	err := errors.New(multilineMessage)
	err = libgit2PushError(err)
	reformattedMessage := err.Error()
	if reformattedMessage != expectedReformat {
		t.Errorf("expected %q, got %q", expectedReformat, reformattedMessage)
	}
}

func TestLibgit2ErrorUnchanged(t *testing.T) {
	// this is (roughly) what GitHub sends if the deploy key doesn't have write access
	regularMessage := `remote: ERROR: deploy key does not have permissions`
	expectedReformat := regularMessage
	err := errors.New(regularMessage)
	err = libgit2PushError(err)
	reformattedMessage := err.Error()
	if reformattedMessage != expectedReformat {
		t.Errorf("expected %q, got %q", expectedReformat, reformattedMessage)
	}
}
