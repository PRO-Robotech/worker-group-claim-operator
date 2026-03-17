package builder

import (
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

var bmtGVK = schema.GroupVersionKind{
	Group:   v1alpha1.BMTGroup,
	Version: v1alpha1.BMTVersion,
	Kind:    v1alpha1.KindBMT,
}

// BuildBMT constructs an unstructured BegetMachineTemplate from rendered YAML.
func BuildBMT(renderedYAML, name, namespace string, claim *v1alpha1.WorkerGroupClaim) (*unstructured.Unstructured, error) {
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(renderedYAML), &spec); err != nil {
		return nil, fmt.Errorf("failed to parse rendered BMT YAML: %w", err)
	}

	// Extract spec from rendered YAML (template output may contain full resource or just spec)
	resourceSpec, ok := spec["spec"]
	if !ok {
		return nil, errors.New("rendered BMT YAML missing 'spec' field")
	}

	bmt := &unstructured.Unstructured{}
	bmt.SetGroupVersionKind(bmtGVK)
	bmt.SetName(name)
	bmt.SetNamespace(namespace)
	bmt.SetLabels(map[string]string{
		v1alpha1.LabelClusterName: claim.Spec.ClusterName,
		v1alpha1.LabelClaimName:   claim.Name,
		v1alpha1.LabelClaimNS:     claim.Namespace,
	})
	bmt.SetOwnerReferences([]metav1.OwnerReference{
		ownerRef(claim),
	})
	bmt.Object["spec"] = resourceSpec

	return bmt, nil
}

func ownerRef(claim *v1alpha1.WorkerGroupClaim) metav1.OwnerReference {
	blockDeletion := true
	isController := true

	return metav1.OwnerReference{
		APIVersion:         v1alpha1.GroupVersion.String(),
		Kind:               "WorkerGroupClaim",
		Name:               claim.Name,
		UID:                claim.UID,
		BlockOwnerDeletion: &blockDeletion,
		Controller:         &isController,
	}
}
