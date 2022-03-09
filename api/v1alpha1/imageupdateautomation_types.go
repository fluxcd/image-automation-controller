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

package v1alpha1

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fluxcd/pkg/apis/meta"
)

const ImageUpdateAutomationKind = "ImageUpdateAutomation"

// ImageUpdateAutomationSpec defines the desired state of ImageUpdateAutomation
type ImageUpdateAutomationSpec struct {
	// Checkout gives the parameters for cloning the git repository,
	// ready to make changes.
	// +required
	Checkout GitCheckoutSpec `json:"checkout"`

	// Interval gives an lower bound for how often the automation
	// run should be attempted.
	// +required
	Interval metav1.Duration `json:"interval"`

	// Update gives the specification for how to update the files in
	// the repository. This can be left empty, to use the default
	// value.
	// +kubebuilder:default={"strategy":"Setters"}
	Update *UpdateStrategy `json:"update,omitempty"`

	// Commit specifies how to commit to the git repository.
	// +required
	Commit CommitSpec `json:"commit"`

	// Push specifies how and where to push commits made by the
	// automation. If missing, commits are pushed (back) to
	// `.spec.checkout.branch`.
	// +optional
	Push *PushSpec `json:"push,omitempty"`

	// Suspend tells the controller to not run this automation, until
	// it is unset (or set to false). Defaults to false.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

type GitCheckoutSpec struct {
	// GitRepositoryRef refers to the resource giving access details
	// to a git repository to update files in.
	// +required
	GitRepositoryRef meta.LocalObjectReference `json:"gitRepositoryRef"`
	// Branch gives the branch to clone from the git repository. If
	// `.spec.push` is not supplied, commits will also be pushed to
	// this branch.
	// +required
	Branch string `json:"branch"`
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

// CommitSpec specifies how to commit changes to the git repository
type CommitSpec struct {
	// AuthorName gives the name to provide when making a commit
	// +required
	AuthorName string `json:"authorName"`
	// AuthorEmail gives the email to provide when making a commit
	// +required
	AuthorEmail string `json:"authorEmail"`
	// SigningKey provides the option to sign commits with a GPG key
	// +optional
	SigningKey *SigningKey `json:"signingKey,omitempty"`
	// MessageTemplate provides a template for the commit message,
	// into which will be interpolated the details of the change made.
	// +optional
	MessageTemplate string `json:"messageTemplate,omitempty"`
}

// PushSpec specifies how and where to push commits.
type PushSpec struct {
	// Branch specifies that commits should be pushed to the branch
	// named. The branch is created using `.spec.checkout.branch` as the
	// starting point, if it doesn't already exist.
	// +required
	Branch string `json:"branch"`
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

// SigningKey references a Kubernetes secret that contains a GPG keypair
type SigningKey struct {
	// SecretRef holds the name to a secret that contains a 'git.asc' key
	// corresponding to the ASCII Armored file containing the GPG signing
	// keypair as the value. It must be in the same namespace as the
	// ImageUpdateAutomation.
	// +required
	SecretRef meta.LocalObjectReference `json:"secretRef,omitempty"`
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
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
	apimeta.SetStatusCondition(auto.GetStatusConditions(), newCondition)
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Last run",type=string,JSONPath=`.status.lastAutomationRunTime`

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

// +kubebuilder:object:root=true

// ImageUpdateAutomationList contains a list of ImageUpdateAutomation
type ImageUpdateAutomationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageUpdateAutomation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImageUpdateAutomation{}, &ImageUpdateAutomationList{})
}
