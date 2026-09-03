package builder

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

var (
	mhcNodeStartupTimeoutSeconds         = int32(360)
	mhcUnhealthyTimeoutSeconds           = int32(600)
	mhcUnhealthySingleNodeTimeoutSeconds = int32(900)
	mhcUnhealthyLessThanOrEqualTo        = intstr.FromInt32(1)
	mdRemediationMaxInFlight             = intstr.FromInt32(1)
)

// MachineHealthCheckName returns the MHC name: {clusterName}-{claimName}-mhc.
func MachineHealthCheckName(clusterName, claimName string) string {
	return MachineDeploymentName(clusterName, claimName) + "-mhc"
}

// BuildMachineHealthCheck returns the desired MachineHealthCheck, or nil when autohealing is off.
func BuildMachineHealthCheck(claim *v1alpha1.WorkerGroupClaim) *clusterv1.MachineHealthCheck {
	if claim.Spec.HealthCheck == nil || !claim.Spec.HealthCheck.Enabled {
		return nil
	}

	timeoutFalse := mhcUnhealthyTimeoutSeconds
	if claim.Spec.Replicas != nil && *claim.Spec.Replicas == 1 {
		timeoutFalse = mhcUnhealthySingleNodeTimeoutSeconds
	}
	timeoutUnknown := timeoutFalse

	startupTimeout := mhcNodeStartupTimeoutSeconds
	threshold := mhcUnhealthyLessThanOrEqualTo

	return &clusterv1.MachineHealthCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:            MachineHealthCheckName(claim.Spec.ClusterName, claim.Name),
			Namespace:       claim.Namespace,
			Labels:          ObjectLabels(claim),
			OwnerReferences: []metav1.OwnerReference{ownerRef(claim)},
		},
		Spec: clusterv1.MachineHealthCheckSpec{
			ClusterName: claim.Spec.ClusterName,
			Selector:    metav1.LabelSelector{MatchLabels: SelectorLabels(claim)},
			Checks: clusterv1.MachineHealthCheckChecks{
				NodeStartupTimeoutSeconds: &startupTimeout,
				UnhealthyNodeConditions: []clusterv1.UnhealthyNodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse, TimeoutSeconds: &timeoutFalse},
					{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, TimeoutSeconds: &timeoutUnknown},
				},
			},
			Remediation: clusterv1.MachineHealthCheckRemediation{
				TriggerIf: clusterv1.MachineHealthCheckRemediationTriggerIf{
					UnhealthyLessThanOrEqualTo: &threshold,
				},
			},
		},
	}
}
