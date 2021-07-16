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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fluxcd/pkg/apis/meta"
)

const ImageUpdateAutomationKind = "ImageUpdateAutomation"

// ImageUpdateAutomationSpec defines the desired state of ImageUpdateAutomation
type ImageUpdateAutomationSpec struct {
	// SourceRef refers to the resource giving access details
	// to a git repository. It must be in the same namespace as the
	// ImageUpdateAutomation.
	// +required
	SourceRef SourceReference `json:"sourceRef"`
	// GitSpec contains all the git-specific definitions. This is
	// technically optional, but in practice mandatory until there are
	// other kinds of source allowed.
	// +optional
	GitSpec *GitSpec `json:"git,omitempty"`

	// Interval gives an lower bound for how often the automation
	// run should be attempted.
	// +required
	Interval metav1.Duration `json:"interval"`

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
	Conditions                  []metav1.Condition `json:"conditions,omitempty"`
	meta.ReconcileRequestStatus `json:",inline"`
}

const (
	// GitNotAvailableReason is used for ConditionReady when the
	// automation run cannot proceed because the git repository is
	// missing or cannot be cloned.
	GitNotAvailableReason = "GitRepositoryNotAvailable"
	// NoStrategyReason is used for ConditionReady when the automation
	// run cannot proceed because there is no update strategy given in
	// the spec.
	NoStrategyReason = "MissingUpdateStrategy"
)

// SetImageUpdateAutomationReadiness sets the ready condition with the given status, reason and message.
func SetImageUpdateAutomationReadiness(auto *ImageUpdateAutomation, status metav1.ConditionStatus, reason, message string) {
	auto.Status.ObservedGeneration = auto.ObjectMeta.Generation
	meta.SetResourceCondition(auto, meta.ReadyCondition, status, reason, message)
}

//+kubebuilder:storageversion
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Last run",type=string,JSONPath=`.status.lastAutomationRunTime`

// ImageUpdateAutomation is the Schema for the imageupdateautomations API
type ImageUpdateAutomation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageUpdateAutomationSpec   `json:"spec,omitempty"`
	Status ImageUpdateAutomationStatus `json:"status,omitempty"`
}

func (auto *ImageUpdateAutomation) GetStatusConditions() *[]metav1.Condition {
	return &auto.Status.Conditions
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
