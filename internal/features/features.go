/*
Copyright 2022 The Flux authors

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

// Package features sets the feature gates that
// source-controller supports, and their default
// states.
package features

import feathelper "github.com/fluxcd/pkg/runtime/features"

const (
	// GitForcePushBranch enables the use of "force push" when push branches
	// are configured.
	GitForcePushBranch = "GitForcePushBranch"

	// ForceGoGitImplementation ignores the value set for gitImplementation
	// of a GitRepository object and ensures that go-git is used for all git operations.
	//
	// When enabled, libgit2 won't be initialized, nor will any git2go cgo
	// code be called.
	ForceGoGitImplementation = "ForceGoGitImplementation"
)

var features = map[string]bool{
	// GitForcePushBranch
	// opt-out from v0.27
	GitForcePushBranch: true,

	// ForceGoGitImplementation
	// opt-out from v0.27
	ForceGoGitImplementation: true,
}

// DefaultFeatureGates contains a list of all supported feature gates and
// their default values.
func FeatureGates() map[string]bool {
	return features
}

// Enabled verifies whether the feature is enabled or not.
//
// This is only a wrapper around the Enabled func in
// pkg/runtime/features, so callers won't need to import
// both packages for checking whether a feature is enabled.
func Enabled(feature string) (bool, error) {
	return feathelper.Enabled(feature)
}
