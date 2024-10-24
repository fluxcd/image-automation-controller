/*
Copyright 2024 The Flux authors

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

package v1beta2

import (
	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

type GitSpec struct {
	// Checkout gives the parameters for cloning the git repository,
	// ready to make changes. If not present, the `spec.ref` field from the
	// referenced `GitRepository` or its default will be used.
	// +optional
	Checkout *GitCheckoutSpec `json:"checkout,omitempty"`

	// Commit specifies how to commit to the git repository.
	// +required
	Commit CommitSpec `json:"commit"`

	// Push specifies how and where to push commits made by the
	// automation. If missing, commits are pushed (back) to
	// `.spec.checkout.branch` or its default.
	// +optional
	Push *PushSpec `json:"push,omitempty"`
}

// HasRefspec returns if the GitSpec has a Refspec.
func (gs GitSpec) HasRefspec() bool {
	if gs.Push == nil {
		return false
	}
	return gs.Push.Refspec != ""
}

type GitCheckoutSpec struct {
	// Reference gives a branch, tag or commit to clone from the Git
	// repository.
	// +required
	Reference sourcev1.GitRepositoryRef `json:"ref"`
}

// CommitSpec specifies how to commit changes to the git repository
type CommitSpec struct {
	// Author gives the email and optionally the name to use as the
	// author of commits.
	// +required
	Author CommitUser `json:"author"`
	// SigningKey provides the option to sign commits with a GPG key
	// +optional
	SigningKey *SigningKey `json:"signingKey,omitempty"`
	// MessageTemplate provides a template for the commit message,
	// into which will be interpolated the details of the change made.
	// +optional
	MessageTemplate string `json:"messageTemplate,omitempty"`

	// MessageTemplateValues provides additional values to be available to the
	// templating rendering.
	MessageTemplateValues map[string]string `json:"messageTemplateValues,omitempty"`
}

type CommitUser struct {
	// Name gives the name to provide when making a commit.
	// +optional
	Name string `json:"name,omitempty"`
	// Email gives the email to provide when making a commit.
	// +required
	Email string `json:"email"`
}

// SigningKey references a Kubernetes secret that contains a GPG keypair
type SigningKey struct {
	// SecretRef holds the name to a secret that contains a 'git.asc' key
	// corresponding to the ASCII Armored file containing the GPG signing
	// keypair as the value. It must be in the same namespace as the
	// ImageUpdateAutomation.
	// +required
	SecretRef meta.LocalObjectReference `json:"secretRef,omitempty"`
}

// PushSpec specifies how and where to push commits.
type PushSpec struct {
	// Branch specifies that commits should be pushed to the branch
	// named. The branch is created using `.spec.checkout.branch` as the
	// starting point, if it doesn't already exist.
	// +optional
	Branch string `json:"branch,omitempty"`

	// Refspec specifies the Git Refspec to use for a push operation.
	// If both Branch and Refspec are provided, then the commit is pushed
	// to the branch and also using the specified refspec.
	// For more details about Git Refspecs, see:
	// https://git-scm.com/book/en/v2/Git-Internals-The-Refspec
	// +optional
	Refspec string `json:"refspec,omitempty"`

	// Options specifies the push options that are sent to the Git
	// server when performing a push operation. For details, see:
	// https://git-scm.com/docs/git-push#Documentation/git-push.txt---push-optionltoptiongt
	// +optional
	Options map[string]string `json:"options,omitempty"`
}
