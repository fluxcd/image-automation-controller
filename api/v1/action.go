/*
Copyright 2026 The Flux authors

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

package v1

// Action describes an observable stage of the reconcile loop, from listing
// image policies through configuring the source, applying setter updates and
// pushing the resulting commit.
type Action string

// String returns the string representation of the Action.
func (a Action) String() string {
	return string(a)
}

const (
	// ActionReconcile denotes the overall outcome of the reconcile loop,
	// emitted once per run to report that reconciliation finished or failed.
	ActionReconcile Action = "Reconcile"

	// ActionListPolicies lists the ImagePolicy objects in the automation's
	// namespace, optionally filtered by spec.policySelector, and records the
	// latest image ref reported by each policy.
	ActionListPolicies Action = "ListPolicies"

	// ActionConfigureSource resolves the referenced GitRepository and its
	// authentication, enforcing cross-namespace access controls before the
	// source manager is built.
	ActionConfigureSource Action = "ConfigureSource"

	// ActionApplyPolicies runs the Setters update strategy over the files under
	// spec.update.path, replacing marked field values with the latest image
	// refs.
	ActionApplyPolicies Action = "ApplyPolicies"

	// ActionCommitAndPush renders the commit message template, commits the
	// setter changes with optional GPG signing, and pushes to the configured
	// branch and/or refspec.
	ActionCommitAndPush Action = "CommitAndPush"
)
