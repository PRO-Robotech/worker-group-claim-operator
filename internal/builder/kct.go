package builder

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

// BuildKCT constructs a typed KubeadmConfigTemplate from rendered YAML.
func BuildKCT(renderedYAML, name, namespace string, claim *v1alpha1.WorkerGroupClaim) (*bootstrapv1.KubeadmConfigTemplate, error) {
	var rendered bootstrapv1.KubeadmConfigTemplate
	if err := yaml.Unmarshal([]byte(renderedYAML), &rendered); err != nil {
		return nil, fmt.Errorf("failed to parse rendered KCT YAML: %w", err)
	}

	kct := &bootstrapv1.KubeadmConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha1.LabelClusterName: claim.Spec.ClusterName,
				v1alpha1.LabelClaimName:   claim.Name,
				v1alpha1.LabelClaimNS:     claim.Namespace,
			},
			OwnerReferences: []metav1.OwnerReference{
				ownerRef(claim),
			},
		},
		Spec: rendered.Spec,
	}

	return kct, nil
}
