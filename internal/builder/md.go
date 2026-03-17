package builder

import (
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

// BuildMachineDeployment constructs a MachineDeployment from WorkerGroupClaim spec.
func BuildMachineDeployment(
	claim *v1alpha1.WorkerGroupClaim,
	bmtName, kctName string,
) *clusterv1.MachineDeployment {
	mdName := MachineDeploymentName(claim.Spec.ClusterName, claim.Name)

	// System labels for selector and template
	selectorLabels := map[string]string{
		v1alpha1.LabelClusterName: claim.Spec.ClusterName,
		v1alpha1.LabelClaimName:   claim.Name,
	}

	// Template labels = system + user nodeLabels
	templateLabels := make(map[string]string, len(selectorLabels)+len(claim.Spec.NodeLabels))
	maps.Copy(templateLabels, selectorLabels)
	maps.Copy(templateLabels, claim.Spec.NodeLabels)

	md := &clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mdName,
			Namespace: claim.Namespace,
			Labels: map[string]string{
				v1alpha1.LabelClusterName: claim.Spec.ClusterName,
				v1alpha1.LabelClaimName:   claim.Name,
				v1alpha1.LabelClaimNS:     claim.Namespace,
			},
			OwnerReferences: []metav1.OwnerReference{
				ownerRef(claim),
			},
		},
		Spec: clusterv1.MachineDeploymentSpec{
			ClusterName: claim.Spec.ClusterName,
			Replicas:    claim.Spec.Replicas,
			Selector: metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: templateLabels,
				},
				Spec: clusterv1.MachineSpec{
					ClusterName: claim.Spec.ClusterName,
					Version:     claim.Spec.Version,
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: clusterv1.ContractVersionedObjectReference{
							APIGroup: v1alpha1.KCTGroup,
							Kind:     v1alpha1.KindKCT,
							Name:     kctName,
						},
					},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: v1alpha1.BMTGroup,
						Kind:     v1alpha1.KindBMT,
						Name:     bmtName,
					},
					Taints: convertTaints(claim.Spec.Taints),
				},
			},
		},
	}

	// Machine deletion timeouts — defaults + optional overrides from spec.deletion
	md.Spec.Template.Spec.Deletion = buildMachineDeletion(claim.Spec.Deletion)

	// Strategy mapping
	if claim.Spec.Strategy != nil {
		md.Spec.Rollout.Strategy = convertStrategy(claim.Spec.Strategy)

		// deletePolicy → spec.deletion.order
		if claim.Spec.Strategy.RollingUpdate != nil && claim.Spec.Strategy.RollingUpdate.DeletePolicy != nil {
			md.Spec.Deletion = clusterv1.MachineDeploymentDeletionSpec{
				Order: clusterv1.MachineSetDeletionOrder(*claim.Spec.Strategy.RollingUpdate.DeletePolicy),
			}
		}
	}

	return md
}

// MachineDeploymentName returns the standard MD name: {clusterName}-{claimName}.
func MachineDeploymentName(clusterName, claimName string) string {
	return clusterName + "-" + claimName
}

// Default Machine deletion timeouts.
var (
	defaultNodeDrainTimeoutSeconds        = int32(60)
	defaultNodeVolumeDetachTimeoutSeconds = int32(60)
	defaultNodeDeletionTimeoutSeconds     = int32(120)
)

// buildMachineDeletion returns MachineDeletionSpec with hardcoded defaults,
// overridden by user-provided values from spec.deletion if present.
func buildMachineDeletion(cfg *v1alpha1.MachineDeletionConfig) clusterv1.MachineDeletionSpec {
	spec := clusterv1.MachineDeletionSpec{
		NodeDrainTimeoutSeconds:        &defaultNodeDrainTimeoutSeconds,
		NodeVolumeDetachTimeoutSeconds: &defaultNodeVolumeDetachTimeoutSeconds,
		NodeDeletionTimeoutSeconds:     &defaultNodeDeletionTimeoutSeconds,
	}
	if cfg == nil {
		return spec
	}
	if cfg.NodeDrainTimeoutSeconds != nil {
		spec.NodeDrainTimeoutSeconds = cfg.NodeDrainTimeoutSeconds
	}
	if cfg.NodeVolumeDetachTimeoutSeconds != nil {
		spec.NodeVolumeDetachTimeoutSeconds = cfg.NodeVolumeDetachTimeoutSeconds
	}
	if cfg.NodeDeletionTimeoutSeconds != nil {
		spec.NodeDeletionTimeoutSeconds = cfg.NodeDeletionTimeoutSeconds
	}

	return spec
}

func convertStrategy(s *v1alpha1.MachineDeploymentStrategy) clusterv1.MachineDeploymentRolloutStrategy {
	rs := clusterv1.MachineDeploymentRolloutStrategy{
		Type: clusterv1.MachineDeploymentRolloutStrategyType(s.Type),
	}
	if s.RollingUpdate != nil {
		rs.RollingUpdate = clusterv1.MachineDeploymentRolloutStrategyRollingUpdate{
			MaxSurge:       s.RollingUpdate.MaxSurge,
			MaxUnavailable: s.RollingUpdate.MaxUnavailable,
		}
	}

	return rs
}

func convertTaints(taints []v1alpha1.MachineTaint) []clusterv1.MachineTaint {
	if len(taints) == 0 {
		return nil
	}
	result := make([]clusterv1.MachineTaint, len(taints))
	for i, t := range taints {
		result[i] = clusterv1.MachineTaint{
			Key:         t.Key,
			Value:       t.Value,
			Effect:      t.Effect,
			Propagation: clusterv1.MachineTaintPropagation(t.Propagation),
		}
	}

	return result
}

// Helper to get a pointer to string. Used in tests.
func stringPtr(s string) *string {
	return &s
}
