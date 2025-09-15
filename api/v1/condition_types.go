/*
Copyright 2025 The Flux authors

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

const (
	// InvalidUpdateStrategyReason represents an invalid image update strategy
	// configuration.
	InvalidUpdateStrategyReason string = "InvalidUpdateStrategy"

	// InvalidSourceConfigReason represents an invalid source configuration.
	InvalidSourceConfigReason string = "InvalidSourceConfiguration"

	// SourceManagerFailedReason represents a failure in the SourceManager which
	// manages the source.
	SourceManagerFailedReason string = "SourceManagerFailed"

	// GitOperationFailedReason represents a failure in Git source operation.
	GitOperationFailedReason string = "GitOperationFailed"

	// UpdateFailedReason represents a failure during source update.
	UpdateFailedReason string = "UpdateFailed"

	// InvalidPolicySelectorReason represents an invalid policy selector.
	InvalidPolicySelectorReason string = "InvalidPolicySelector"

	// RemovedTemplateFieldReason represents usage of removed template field.
	RemovedTemplateFieldReason string = "RemovedTemplateField"
)
