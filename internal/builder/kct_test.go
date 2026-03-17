package builder

import (
	"testing"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

func TestBuildKCT_Basic(t *testing.T) {
	renderedYAML := `apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: KubeadmConfigTemplate
spec:
  template:
    spec:
      files:
        - path: /etc/test
          content: "hello"
      joinConfiguration:
        nodeRegistration:
          kubeletExtraArgs:
            - name: node-labels
              value: "app=test"`

	claim := testClaim()
	kct, err := BuildKCT(renderedYAML, "my-cluster-pool-1-kct-deadbeef", "team-a", claim)
	if err != nil {
		t.Fatalf("BuildKCT: %v", err)
	}

	// Name
	if kct.Name != "my-cluster-pool-1-kct-deadbeef" {
		t.Errorf("name = %s", kct.Name)
	}

	// Labels
	if kct.Labels[v1alpha1.LabelClusterName] != "my-cluster" {
		t.Errorf("cluster-name label = %s", kct.Labels[v1alpha1.LabelClusterName])
	}
	if kct.Labels[v1alpha1.LabelClaimName] != "pool-1" {
		t.Errorf("claim-name label = %s", kct.Labels[v1alpha1.LabelClaimName])
	}

	// OwnerRef
	if len(kct.OwnerReferences) != 1 || kct.OwnerReferences[0].Kind != "WorkerGroupClaim" {
		t.Errorf("ownerRefs = %v", kct.OwnerReferences)
	}
}

func TestBuildKCT_InvalidYAML(t *testing.T) {
	_, err := BuildKCT("invalid: yaml: [broken", "name", "ns", testClaim())
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
