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
	// GitManagedTransport implements a managed transport for GitRepository
	// objects that use the libgit2 implementation.
	//
	// When enabled, improves the reliability of libgit2 reconciliations,
	// by enforcing timeouts and ensuring libgit2 cannot hijack the process
	// and hang it indefinitely.
	GitManagedTransport = "GitManagedTransport"
)

var features = map[string]bool{
	// GitManagedTransport
	// opt-in from v0.21 (via environment variable)
	// opt-out from v0.23
	GitManagedTransport: true,
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
