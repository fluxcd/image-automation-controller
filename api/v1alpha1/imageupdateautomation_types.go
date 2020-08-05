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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ImageUpdateAutomationSpec defines the desired state of ImageUpdateAutomation
type ImageUpdateAutomationSpec struct {
	// Checkout gives the parameters for cloning the git repository,
	// ready to make changes.
	// +required
	Checkout GitCheckoutSpec `json:"checkout"`
	// RunInterval gives a lower bound for how often the automation
	// run should be attempted. Otherwise it will default.
	// +optional
	RunInterval *metav1.Duration `json:"minimumRunInterval,omitempty"`
	// Update gives the specification for how to update the files in
	// the repository
	// +required
	Update UpdateStrategy `json:"update"`
	// Commit specifies how to commit to the git repo
	// +required
	Commit CommitSpec `json:"commit"`
}

type GitCheckoutSpec struct {
	// GitRepository refers to the resource giving access details to a
	// git repository to update files in.
	// +required
	GitRepositoryRef corev1.LocalObjectReference `json:"gitRepositoryRef"`
	// Branch gives the branch to clone from the git repository. If
	// missing, it will be left to default; be aware this may give
	// indeterminate results.
	// +optional
	Branch string `json:"branch,omitempty"`
}

// UpdateStrategy is a union of the various strategies for updating
// the git repository.
type UpdateStrategy struct {
	// ImagePolicy if present means update all workloads using the
	// given policy's image, to the policy's latest image reference.
	// +optional
	ImagePolicy *corev1.LocalObjectReference `json:"imagePolicy,omitempty"`
}

// CommitSpec specifies how to commit changes to the git repository
type CommitSpec struct {
	// AuthorName gives the name to provide when making a commit
	// +required
	AuthorName string `json:"authorName"`
	// AuthorEmail gives the email to provide when making a commit
	// +required
	AuthorEmail string `json:"authorEmail"`
	// MessageTemplate provides a template for the commit message,
	// into which will be interpolated the details of the change made.
	// +optional
	MessageTemplate string `json:"messageTemplate,omitempty"`
}

// ImageUpdateAutomationStatus defines the observed state of ImageUpdateAutomation
type ImageUpdateAutomationStatus struct {
	// LastAutomationRunTime records the last time the controller ran
	// this automation through to completion (even if no updates were
	// made).
	// +optional
	LastAutomationRunTime *metav1.Time `json:"lastAutomationRunTime,omitempty"`
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
