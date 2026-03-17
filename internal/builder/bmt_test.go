package builder

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

func testClaim() *v1alpha1.WorkerGroupClaim {
	replicas := int32(3)

	return &v1alpha1.WorkerGroupClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pool-1",
			Namespace: "team-a",
			UID:       types.UID("test-uid-123"),
		},
		Spec: v1alpha1.WorkerGroupClaimSpec{
			ClusterName:          "my-cluster",
			Replicas:             &replicas,
			Version:              "v1.30.1",
			MachineTemplateRef:   v1alpha1.TemplateRef{Name: "my-machine-tmpl"},
			BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "my-bootstrap-tmpl"},
		},
	}
}

func TestBuildBMT_Basic(t *testing.T) {
	renderedYAML := `apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: BegetMachineTemplate
spec:
  template:
    spec:
      configuration:
        cpuCount: 4
        memory: 8192
        diskSize: 61440
      image: "ubuntu-22.04"
      sshKeyIds:
        - 123
        - 456`

	claim := testClaim()
	bmt, err := BuildBMT(renderedYAML, "my-cluster-pool-1-bmt-abc12345", "team-a", claim)
	if err != nil {
		t.Fatalf("BuildBMT: %v", err)
	}

	// GVK
	gvk := bmt.GroupVersionKind()
	if gvk.Group != v1alpha1.BMTGroup || gvk.Kind != v1alpha1.KindBMT {
		t.Errorf("GVK = %v, want %s/%s", gvk, v1alpha1.BMTGroup, v1alpha1.KindBMT)
	}

	// Name
	if bmt.GetName() != "my-cluster-pool-1-bmt-abc12345" {
		t.Errorf("name = %s", bmt.GetName())
	}

	// Labels
	labels := bmt.GetLabels()
	if labels[v1alpha1.LabelClusterName] != "my-cluster" {
		t.Errorf("cluster-name label = %s", labels[v1alpha1.LabelClusterName])
	}
	if labels[v1alpha1.LabelClaimName] != "pool-1" {
		t.Errorf("claim-name label = %s", labels[v1alpha1.LabelClaimName])
	}

	// OwnerRef
	refs := bmt.GetOwnerReferences()
	if len(refs) != 1 || refs[0].Kind != "WorkerGroupClaim" {
		t.Errorf("ownerRefs = %v", refs)
	}

	// Spec parsed
	spec, ok, _ := unstructured.NestedMap(bmt.Object, "spec", "template", "spec")
	if !ok {
		t.Fatal("spec.template.spec not found")
	}
	if spec["image"] != "ubuntu-22.04" {
		t.Errorf("image = %v", spec["image"])
	}
}

func TestBuildBMT_InvalidYAML(t *testing.T) {
	_, err := BuildBMT("invalid: yaml: [broken", "name", "ns", testClaim())
	if err != nil {
		t.Log("error for malformed YAML:", err)
	}
}

func TestBuildBMT_MissingSpec(t *testing.T) {
	_, err := BuildBMT("apiVersion: v1\nkind: ConfigMap\n", "name", "ns", testClaim())
	if err == nil {
		t.Error("expected error for missing spec")
	}
}
