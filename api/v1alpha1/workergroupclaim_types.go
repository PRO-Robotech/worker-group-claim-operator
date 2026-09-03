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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Phase constants for WorkerGroupClaim lifecycle.
const (
	PhaseProvisioning = "Provisioning"
	PhaseReady        = "Ready"
	PhaseUpdating     = "Updating"
	PhaseDegraded     = "Degraded"
	PhaseFailed       = "Failed"
	PhasePaused       = "Paused"
	PhaseDeleting     = "Deleting"
)

// Condition type constants for WorkerGroupClaim.
const (
	ConditionReady             = "Ready"
	ConditionTemplatesRendered = "TemplatesRendered"
	ConditionRolloutComplete   = "RolloutComplete"
	ConditionRolloutTimedOut   = "RolloutTimedOut"
	ConditionPaused            = "Paused"

	ConditionNodeLabelsAccepted = "NodeLabelsAccepted"

	ConditionAutohealingConfigured = "AutohealingConfigured"
)

// Finalizer for cleanup on deletion.
const (
	WorkerGroupClaimFinalizer = "workergroup.in-cloud.io/finalizer"
)

// Annotation for pausing reconciliation.
const (
	PausedAnnotation = "workergroup.in-cloud.io/paused"
)

// Label constants for managed resources.
const (
	LabelClaimName   = "workergroup.in-cloud.io/claim-name"
	LabelClaimNS     = "workergroup.in-cloud.io/claim-namespace"
	LabelClusterName = "cluster.x-k8s.io/cluster-name"
)

// Resource Kind, APIGroup and Version constants for managed Beget/Bootstrap resources.
const (
	KindBMT     = "BegetMachineTemplate"
	KindBMTList = "BegetMachineTemplateList"
	KindKCT     = "KubeadmConfigTemplate"
	KindMHC     = "MachineHealthCheck"

	BMTGroup   = "infrastructure.cluster.x-k8s.io"
	BMTVersion = "v1beta2"
	KCTGroup   = "bootstrap.cluster.x-k8s.io"
)

// WorkerGroupClaimSpec defines the desired state of WorkerGroupClaim.
type WorkerGroupClaimSpec struct {
	// ClusterName is the name of the ClusterAPI Cluster.
	// Immutable after creation.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterName is immutable"
	ClusterName string `json:"clusterName"`

	// Replicas is the desired number of machines. In-place MD update.
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas"`

	// Version is the Kubernetes version (e.g., "v1.30.1"). In-place MD update.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// MachineTemplateRef references a cluster-scoped WGMachineTemplate.
	MachineTemplateRef TemplateRef `json:"machineTemplateRef"`

	// BootstrapTemplateRef references a cluster-scoped WGBootstrapTemplate.
	BootstrapTemplateRef TemplateRef `json:"bootstrapTemplateRef"`

	// Infrastructure variables rendered through WGMachineTemplate.
	// Values can be: string, number, boolean, array, or object.
	// Each key maps to {{ .key }} in the template.
	// +optional
	Infrastructure map[string]apiextensionsv1.JSON `json:"infrastructure,omitempty"`

	// Bootstrap variables rendered through WGBootstrapTemplate.
	// Values can be: string, number, boolean, array, or object.
	// Each key maps to {{ .key }} in the template.
	// Operator auto-injects "nodeLabels" from spec.nodeLabels and
	// "machineDeploymentName" = {clusterName}-{claimName} if not set here.
	// +optional
	Bootstrap map[string]apiextensionsv1.JSON `json:"bootstrap,omitempty"`

	// NodeLabels are labels applied to Kubernetes nodes.
	// Dual effect: added to MD template labels AND auto-injected as
	// "nodeLabels" bootstrap var (format: "k1=v1,k2=v2", sorted keys).
	// Keys using reserved prefixes (cluster.x-k8s.io/, workergroup.in-cloud.io/,
	// node-group.beget.com/, node.cluster.x-k8s.io/, node-restriction.kubernetes.io/)
	// are ignored — see the NodeLabelsAccepted condition.
	// Syntactically invalid keys or values fail the claim.
	// +optional
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`

	// Taints are applied to nodes via MachineDeployment.spec.template.spec.taints.
	// In-place MD update — does NOT trigger rollout (no node recreation).
	// +optional
	// +listType=atomic
	Taints []MachineTaint `json:"taints,omitempty"`

	// Strategy defines the rolling update strategy.
	// Mapped 1:1 to MachineDeployment.spec.strategy. In-place MD update.
	// +optional
	Strategy *MachineDeploymentStrategy `json:"strategy,omitempty"`

	// Deletion configures timeouts for Machine deletion (drain, volume detach, node deletion).
	// Maps to MachineDeployment.spec.template.spec.deletion.
	// If not set, defaults are applied: nodeDrainTimeoutSeconds=60, nodeVolumeDetachTimeoutSeconds=60, nodeDeletionTimeoutSeconds=120.
	// In-place MD update — does NOT trigger rollout.
	// +optional
	Deletion *MachineDeletionConfig `json:"deletion,omitempty"`

	// KubeletConfiguration overrides for kubelet config on worker nodes.
	// Merged with default kubelet config and injected as __kubeletConfigYaml
	// into bootstrap vars. Changes update KCT in-place, NO rollout.
	// +optional
	KubeletConfiguration *KubeletConfigurationOverrides `json:"kubeletConfiguration,omitempty"`

	// RolloutTimeout is the maximum duration to wait for a rollout to complete.
	// After this, condition RolloutTimedOut=True is set and phase becomes Degraded.
	// No automatic rollback. Default: 30m.
	// +kubebuilder:default="30m"
	// +optional
	RolloutTimeout *metav1.Duration `json:"rolloutTimeout,omitempty"`

	// HealthCheck enables automatic replacement of unhealthy nodes.
	// +optional
	HealthCheck *WorkerGroupHealthCheck `json:"healthCheck,omitempty"`
}

// WorkerGroupHealthCheck configures automatic node remediation for the group.
type WorkerGroupHealthCheck struct {
	// Enabled turns automatic node replacement on for this worker group.
	Enabled bool `json:"enabled"`
}

// TemplateRef is a reference to a cluster-scoped template resource.
type TemplateRef struct {
	// Name of the cluster-scoped template resource.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// MachineTaint mirrors ClusterAPI v1beta2 MachineTaint type.
type MachineTaint struct {
	// Key is the taint key.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=317
	Key string `json:"key"`

	// Value is the taint value.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Value string `json:"value,omitempty"`

	// Effect is the taint effect.
	// +kubebuilder:validation:Enum=NoSchedule;PreferNoSchedule;NoExecute
	Effect corev1.TaintEffect `json:"effect"`

	// Propagation defines how the taint is propagated to nodes.
	// Always: continuously reconciled. OnInitialization: set only on node init.
	// +kubebuilder:default=Always
	// +kubebuilder:validation:Enum=Always;OnInitialization
	Propagation string `json:"propagation"`
}

// MachineDeploymentStrategy defines the strategy for MachineDeployment updates.
type MachineDeploymentStrategy struct {
	// Type of deployment strategy. Default: RollingUpdate.
	// +kubebuilder:default=RollingUpdate
	// +kubebuilder:validation:Enum=RollingUpdate;OnDelete
	// +optional
	Type string `json:"type,omitempty"`

	// RollingUpdate config params. Present only if Type = RollingUpdate.
	// +optional
	RollingUpdate *RollingUpdateStrategy `json:"rollingUpdate,omitempty"`
}

// RollingUpdateStrategy defines the rolling update parameters.
type RollingUpdateStrategy struct {
	// MaxSurge is the maximum number of machines that can be created
	// above the desired number during update. Value can be an absolute
	// number (e.g., 5) or a percentage (e.g., "10%").
	// +optional
	MaxSurge *intstr.IntOrString `json:"maxSurge,omitempty"`

	// MaxUnavailable is the maximum number of machines that can be
	// unavailable during update. Value can be an absolute number or percentage.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// DeletePolicy defines the policy for selecting which machines to delete.
	// +kubebuilder:validation:Enum=Random;Newest;Oldest
	// +optional
	DeletePolicy *string `json:"deletePolicy,omitempty"`
}

// MachineDeletionConfig configures timeouts for Machine deletion.
type MachineDeletionConfig struct {
	// NodeDrainTimeoutSeconds is the total time to spend draining a node. Default: 60.
	// +kubebuilder:validation:Minimum=0
	// +optional
	NodeDrainTimeoutSeconds *int32 `json:"nodeDrainTimeoutSeconds,omitempty"`

	// NodeVolumeDetachTimeoutSeconds is the total time to wait for volumes to detach. Default: 60.
	// +kubebuilder:validation:Minimum=0
	// +optional
	NodeVolumeDetachTimeoutSeconds *int32 `json:"nodeVolumeDetachTimeoutSeconds,omitempty"`

	// NodeDeletionTimeoutSeconds is the total time to attempt Node deletion. Default: 120.
	// +kubebuilder:validation:Minimum=0
	// +optional
	NodeDeletionTimeoutSeconds *int32 `json:"nodeDeletionTimeoutSeconds,omitempty"`
}

// KubeletConfigurationOverrides contains kubelet config fields that can be
// overridden per worker group. Merged with default kubelet config.
type KubeletConfigurationOverrides struct {
	// +optional
	MaxPods *int32 `json:"maxPods,omitempty"`
	// +optional
	CpuCFSQuota *bool `json:"cpuCFSQuota,omitempty"`
	// +optional
	ImageMinimumGCAge *string `json:"imageMinimumGCAge,omitempty"`
	// +optional
	ImageMaximumGCAge *string `json:"imageMaximumGCAge,omitempty"`
	// +optional
	ImageGCHighThresholdPercent *int32 `json:"imageGCHighThresholdPercent,omitempty"`
	// +optional
	ImageGCLowThresholdPercent *int32 `json:"imageGCLowThresholdPercent,omitempty"`
	// +optional
	ShutdownGracePeriod *string `json:"shutdownGracePeriod,omitempty"`
	// +optional
	KubeReserved *ResourceOverrides `json:"kubeReserved,omitempty"`
	// +optional
	SystemReserved map[string]string `json:"systemReserved,omitempty"`
	// +optional
	EvictionHard map[string]string `json:"evictionHard,omitempty"`
	// +optional
	EvictionSoft map[string]string `json:"evictionSoft,omitempty"`
	// +optional
	EvictionSoftGracePeriod map[string]string `json:"evictionSoftGracePeriod,omitempty"`
	// +optional
	EvictionPressureTransitionPeriod *string `json:"evictionPressureTransitionPeriod,omitempty"`
	// +optional
	EvictionMaxPodGracePeriod *int32 `json:"evictionMaxPodGracePeriod,omitempty"`
	// +optional
	EvictionMinimumReclaim map[string]string `json:"evictionMinimumReclaim,omitempty"`
}

// ResourceOverrides defines CPU and memory resource limits.
type ResourceOverrides struct {
	// +optional
	CPU *string `json:"cpu,omitempty"`
	// +optional
	Memory *string `json:"memory,omitempty"`
}

// HealthCheckStatus is the observed state of the MachineHealthCheck owned by the claim.
type HealthCheckStatus struct {
	// Enabled reports whether the MachineHealthCheck exists.
	Enabled bool `json:"enabled"`

	// Name of the MachineHealthCheck object.
	// +optional
	Name string `json:"name,omitempty"`

	// RemediationAllowed mirrors the RemediationAllowed condition of the MachineHealthCheck.
	// +optional
	RemediationAllowed *bool `json:"remediationAllowed,omitempty"`
}

// WorkerGroupClaimStatus defines the observed state of WorkerGroupClaim.
type WorkerGroupClaimStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase represents the current lifecycle phase of the WorkerGroupClaim.
	// +kubebuilder:validation:Enum=Provisioning;Ready;Updating;Degraded;Failed;Paused;Deleting
	// +optional
	Phase string `json:"phase,omitempty"`

	// CurrentTemplates holds names of the currently active BMT and KCT.
	// +optional
	CurrentTemplates *CurrentTemplates `json:"currentTemplates,omitempty"`

	// PendingDeletion is a list of old template resources awaiting cleanup
	// after rollout completes.
	// +optional
	// +listType=atomic
	PendingDeletion []ResourceRef `json:"pendingDeletion,omitempty"`

	// LastRendered holds the hashes and any errors from the last render cycle.
	// +optional
	LastRendered *LastRendered `json:"lastRendered,omitempty"`

	// RolloutStartedAt is the timestamp when the current rollout began.
	// Set when phase transitions to Updating.
	// +optional
	RolloutStartedAt *metav1.Time `json:"rolloutStartedAt,omitempty"`

	// MachineDeployment mirrors relevant status from the managed MachineDeployment.
	// +optional
	MachineDeployment *MachineDeploymentStatus `json:"machineDeployment,omitempty"`

	// HealthCheck mirrors the observed state of the managed MachineHealthCheck.
	// +optional
	HealthCheck *HealthCheckStatus `json:"healthCheck,omitempty"`

	// Conditions represent the current state of the WorkerGroupClaim.
	// Known types: Ready, TemplatesRendered, RolloutComplete, RolloutTimedOut,
	// Paused, NodeLabelsAccepted, AutohealingConfigured.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CurrentTemplates holds references to the currently active template resources.
type CurrentTemplates struct {
	// BMT is the name of the current BegetMachineTemplate.
	BMT string `json:"bmt"`
	// KCT is the name of the current KubeadmConfigTemplate.
	KCT string `json:"kct"`
}

// ResourceRef identifies a Kubernetes resource for pending deletion tracking.
type ResourceRef struct {
	// Kind of the resource (e.g., "BegetMachineTemplate", "KubeadmConfigTemplate").
	Kind string `json:"kind"`
	// Name of the resource.
	Name string `json:"name"`
}

// LastRendered holds the results of the last template rendering cycle.
type LastRendered struct {
	// BMTHash is the SHA256[:8] hash of the rendered BegetMachineTemplate.
	// +optional
	BMTHash string `json:"bmtHash,omitempty"`
	// KCTHash is the SHA256[:8] hash of the rendered KubeadmConfigTemplate.
	// +optional
	KCTHash string `json:"kctHash,omitempty"`
	// Error is the error message from the last failed render, if any.
	// +optional
	Error string `json:"error,omitempty"`
}

// MachineDeploymentStatus mirrors relevant fields from the managed MachineDeployment status.
type MachineDeploymentStatus struct {
	// Replicas is the total number of machines targeted by this deployment.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// ReadyReplicas is the number of ready machines.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// UpToDateReplicas is the number of machines running the latest template.
	// +optional
	UpToDateReplicas int32 `json:"upToDateReplicas,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.clusterName"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.machineDeployment.readyReplicas"
// +kubebuilder:printcolumn:name="UpToDate",type="integer",JSONPath=".status.machineDeployment.upToDateReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// WorkerGroupClaim is the Schema for the workergroupclaims API.
// It manages a ClusterAPI MachineDeployment with immutable template versioning.
type WorkerGroupClaim struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec WorkerGroupClaimSpec `json:"spec"`

	// +optional
	Status WorkerGroupClaimStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// WorkerGroupClaimList contains a list of WorkerGroupClaim.
type WorkerGroupClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []WorkerGroupClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkerGroupClaim{}, &WorkerGroupClaimList{})
}
