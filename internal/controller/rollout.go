package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// isRolloutComplete checks whether the MachineDeployment rollout has finished.
// Uses v1beta2 conditions (RollingOut on status.conditions) as primary indicator,
// falls back to deprecated v1beta1 status fields.
func isRolloutComplete(md *clusterv1.MachineDeployment) bool {
	if md == nil || md.Spec.Replicas == nil {
		return false
	}

	desiredReplicas := *md.Spec.Replicas

	// Primary: v1beta2 conditions on status.conditions directly
	rollingOut := meta.FindStatusCondition(md.Status.Conditions, clusterv1.RollingOutCondition)
	if rollingOut != nil {
		if rollingOut.Status == metav1.ConditionTrue {
			return false
		}
		// RollingOut=False — check upToDateReplicas
		if md.Status.UpToDateReplicas != nil && *md.Status.UpToDateReplicas == desiredReplicas {
			return true
		}

		return false
	}

	// Fallback: deprecated v1beta1 status fields (needed until CAPI drops v1beta1 support)
	if md.Status.Deprecated != nil && md.Status.Deprecated.V1Beta1 != nil {
		legacy := md.Status.Deprecated.V1Beta1
		if md.Status.Replicas != nil && *md.Status.Replicas == desiredReplicas &&
			legacy.UpdatedReplicas == desiredReplicas && //nolint:staticcheck // v1beta1 fallback
			legacy.UnavailableReplicas == 0 { //nolint:staticcheck // v1beta1 fallback
			return true
		}
	}

	return false
}
