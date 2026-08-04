/*
Copyright 2026.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeLabelPolicyName is the name of the singleton policy object.
const NodeLabelPolicyName = "default"

// Condition types for NodeLabelPolicy.
const (
	ConditionPatternsCompiled = "PatternsCompiled"
)

// NodeLabelPolicySpec defines node label keys the platform refuses.
type NodeLabelPolicySpec struct {
	// DenyPatterns are unanchored regexps matched against label keys.
	// +optional
	// +listType=atomic
	DenyPatterns []string `json:"denyPatterns,omitempty"`
}

// NodeLabelPolicyStatus reports how the policy was compiled.
type NodeLabelPolicyStatus struct {
	// CompiledPatterns is the number of patterns in force.
	// +optional
	CompiledPatterns int `json:"compiledPatterns,omitempty"`

	// SkippedPatterns lists patterns that failed to compile and are ignored.
	// +optional
	// +listType=atomic
	SkippedPatterns []string `json:"skippedPatterns,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Patterns",type=integer,JSONPath=`.status.compiledPatterns`
// +kubebuilder:printcolumn:name="Skipped",type=integer,JSONPath=`.status.skippedPatterns`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NodeLabelPolicy is the cluster-scoped singleton holding refused label keys.
type NodeLabelPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec NodeLabelPolicySpec `json:"spec"`

	// +optional
	Status NodeLabelPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NodeLabelPolicyList contains a list of NodeLabelPolicy.
type NodeLabelPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NodeLabelPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeLabelPolicy{}, &NodeLabelPolicyList{})
}
