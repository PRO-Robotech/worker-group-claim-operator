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

// WGMachineTemplateSpec defines the desired state of WGMachineTemplate.
type WGMachineTemplateSpec struct {
	// Value is the Go template content that produces a BegetMachineTemplate.
	// Variables from WorkerGroupClaim.spec.infrastructure are passed as template data.
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// WGMachineTemplate is a cluster-scoped Go template that produces BegetMachineTemplate resources.
// Referenced by WorkerGroupClaim.spec.machineTemplateRef.
type WGMachineTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec WGMachineTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// WGMachineTemplateList contains a list of WGMachineTemplate.
type WGMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []WGMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WGMachineTemplate{}, &WGMachineTemplateList{})
}
