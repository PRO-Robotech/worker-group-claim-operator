package builder

import (
	"reflect"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// MDDiff represents the differences between desired and existing MachineDeployments.
type MDDiff struct {
	// RefsChanged indicates that infrastructureRef or configRef name changed (triggers rollout).
	RefsChanged bool
	// InPlaceChanged indicates that non-ref fields changed (no rollout).
	InPlaceChanged bool
}

// NeedsUpdate returns true if any changes are detected.
func (d MDDiff) NeedsUpdate() bool {
	return d.RefsChanged || d.InPlaceChanged
}

// ComputeMDDiff compares desired and existing MachineDeployments,
// separating ref changes (which trigger rollout) from in-place changes (which don't).
func ComputeMDDiff(desired, existing *clusterv1.MachineDeployment) MDDiff {
	var diff MDDiff

	// Ref changes → rollout
	if desired.Spec.Template.Spec.InfrastructureRef.Name != existing.Spec.Template.Spec.InfrastructureRef.Name {
		diff.RefsChanged = true
	}
	if desired.Spec.Template.Spec.Bootstrap.ConfigRef.Name != existing.Spec.Template.Spec.Bootstrap.ConfigRef.Name {
		diff.RefsChanged = true
	}

	// In-place changes → no rollout
	if !reflect.DeepEqual(desired.Spec.Replicas, existing.Spec.Replicas) {
		diff.InPlaceChanged = true
	}
	if desired.Spec.Template.Spec.Version != existing.Spec.Template.Spec.Version {
		diff.InPlaceChanged = true
	}
	if !reflect.DeepEqual(desired.Spec.Rollout.Strategy, existing.Spec.Rollout.Strategy) {
		diff.InPlaceChanged = true
	}
	if !reflect.DeepEqual(desired.Spec.Template.Spec.Taints, existing.Spec.Template.Spec.Taints) {
		diff.InPlaceChanged = true
	}
	if !reflect.DeepEqual(desired.Spec.Template.Labels, existing.Spec.Template.Labels) {
		diff.InPlaceChanged = true
	}
	if !reflect.DeepEqual(desired.Spec.Template.Spec.Deletion, existing.Spec.Template.Spec.Deletion) {
		diff.InPlaceChanged = true
	}
	if !reflect.DeepEqual(desired.Spec.Deletion, existing.Spec.Deletion) {
		diff.InPlaceChanged = true
	}

	return diff
}
