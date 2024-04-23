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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fluxcd/pkg/apis/meta"
)

const (
	ImageUpdateAutomationKind      = "ImageUpdateAutomation"
	ImageUpdateAutomationFinalizer = "finalizers.fluxcd.io"
)

// ImageUpdateAutomationSpec defines the desired state of ImageUpdateAutomation
type ImageUpdateAutomationSpec struct {
	// SourceRef refers to the resource giving access details
	// to a git repository.
	// +required
	SourceRef CrossNamespaceSourceReference `json:"sourceRef"`

	// GitSpec contains all the git-specific definitions. This is
	// technically optional, but in practice mandatory until there are
	// other kinds of source allowed.
	// +optional
	GitSpec *GitSpec `json:"git,omitempty"`

	// Interval gives an lower bound for how often the automation
	// run should be attempted.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +required
	Interval metav1.Duration `json:"interval"`

	// PolicySelector allows to filter applied policies based on labels.
	// By default includes all policies in namespace.
	// +optional
	PolicySelector *metav1.LabelSelector `json:"policySelector,omitempty"`

	// Update gives the specification for how to update the files in
	// the repository. This can be left empty, to use the default
	// value.
	// +kubebuilder:default={"strategy":"Setters"}
	Update *UpdateStrategy `json:"update,omitempty"`

	// Suspend tells the controller to not run this automation, until
	// it is unset (or set to false). Defaults to false.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// UpdateStrategyName is the type for names that go in
// .update.strategy. NB the value in the const immediately below.
// +kubebuilder:validation:Enum=Setters
type UpdateStrategyName string

const (
	// UpdateStrategySetters is the name of the update strategy that
	// uses kyaml setters. NB the value in the enum annotation for the
	// type, above.
	UpdateStrategySetters UpdateStrategyName = "Setters"
)

// UpdateStrategy is a union of the various strategies for updating
// the Git repository. Parameters for each strategy (if any) can be
// inlined here.
type UpdateStrategy struct {
	// Strategy names the strategy to be used.
	// +required
	// +kubebuilder:default=Setters
	Strategy UpdateStrategyName `json:"strategy"`

	// Path to the directory containing the manifests to be updated.
	// Defaults to 'None', which translates to the root path
	// of the GitRepositoryRef.
	// +optional
	Path string `json:"path,omitempty"`
}

// ImageUpdateAutomationStatus defines the observed state of ImageUpdateAutomation
type ImageUpdateAutomationStatus struct {
	// LastAutomationRunTime records the last time the controller ran
	// this automation through to completion (even if no updates were
	// made).
	// +optional
	LastAutomationRunTime *metav1.Time `json:"lastAutomationRunTime,omitempty"`
	// LastPushCommit records the SHA1 of the last commit made by the
	// controller, for this automation object
	// +optional
	LastPushCommit string `json:"lastPushCommit,omitempty"`
	// LastPushTime records the time of the last pushed change.
	// +optional
	LastPushTime *metav1.Time `json:"lastPushTime,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedPolicies is the list of observed ImagePolicies that were
	// considered by the ImageUpdateAutomation update process.
	// +optional
	ObservedPolicies ObservedPolicies `json:"observedPolicies,omitempty"`
	// ObservedPolicies []ObservedPolicy `json:"observedPolicies,omitempty"`
	// ObservedSourceRevision is the last observed source revision. This can be
	// used to determine if the source has been updated since last observation.
	// +optional
	ObservedSourceRevision string `json:"observedSourceRevision,omitempty"`

	meta.ReconcileRequestStatus `json:",inline"`
}

// ObservedPolicies is a map of policy name and ImageRef of their latest
// ImageRef.
type ObservedPolicies map[string]ImageRef

//+kubebuilder:storageversion
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Last run",type=string,JSONPath=`.status.lastAutomationRunTime`

// ImageUpdateAutomation is the Schema for the imageupdateautomations API
type ImageUpdateAutomation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImageUpdateAutomationSpec `json:"spec,omitempty"`
	// +kubebuilder:default={"observedGeneration":-1}
	Status ImageUpdateAutomationStatus `json:"status,omitempty"`
}

// GetRequeueAfter returns the duration after which the ImageUpdateAutomation
// must be reconciled again.
func (auto ImageUpdateAutomation) GetRequeueAfter() time.Duration {
	return auto.Spec.Interval.Duration
}

// GetConditions returns the status conditions of the object.
func (auto ImageUpdateAutomation) GetConditions() []metav1.Condition {
	return auto.Status.Conditions
}

// SetConditions sets the status conditions on the object.
func (auto *ImageUpdateAutomation) SetConditions(conditions []metav1.Condition) {
	auto.Status.Conditions = conditions
}

//+kubebuilder:object:root=true

// ImageUpdateAutomationList contains a list of ImageUpdateAutomation
type ImageUpdateAutomationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageUpdateAutomation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImageUpdateAutomation{}, &ImageUpdateAutomationList{})
}
