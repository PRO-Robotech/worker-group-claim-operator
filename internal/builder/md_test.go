package builder

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

func TestBuildMachineDeployment_Basic(t *testing.T) {
	claim := testClaim()
	md := BuildMachineDeployment(claim, claim.Spec.NodeLabels, "bmt-abc", "kct-def")

	// Name
	if md.Name != "my-cluster-pool-1" {
		t.Errorf("name = %s, want my-cluster-pool-1", md.Name)
	}

	// ClusterName
	if md.Spec.ClusterName != "my-cluster" {
		t.Errorf("clusterName = %s", md.Spec.ClusterName)
	}

	// Replicas
	if *md.Spec.Replicas != 3 {
		t.Errorf("replicas = %d", *md.Spec.Replicas)
	}

	// Version
	if md.Spec.Template.Spec.Version != "v1.30.1" {
		t.Errorf("version = %s", md.Spec.Template.Spec.Version)
	}

	// InfrastructureRef
	if md.Spec.Template.Spec.InfrastructureRef.Name != "bmt-abc" {
		t.Errorf("infraRef = %s", md.Spec.Template.Spec.InfrastructureRef.Name)
	}
	if md.Spec.Template.Spec.InfrastructureRef.Kind != v1alpha1.KindBMT {
		t.Errorf("infraRef.Kind = %s", md.Spec.Template.Spec.InfrastructureRef.Kind)
	}

	// ConfigRef
	if md.Spec.Template.Spec.Bootstrap.ConfigRef.Name != "kct-def" {
		t.Errorf("configRef = %s", md.Spec.Template.Spec.Bootstrap.ConfigRef.Name)
	}
	if md.Spec.Template.Spec.Bootstrap.ConfigRef.Kind != v1alpha1.KindKCT {
		t.Errorf("configRef.Kind = %s", md.Spec.Template.Spec.Bootstrap.ConfigRef.Kind)
	}

	// Selector
	if md.Spec.Selector.MatchLabels[v1alpha1.LabelClusterName] != "my-cluster" {
		t.Error("selector missing cluster-name")
	}
	if md.Spec.Selector.MatchLabels[v1alpha1.LabelClaimName] != "pool-1" {
		t.Error("selector missing claim-name")
	}

	// Template labels (system only, no user nodeLabels)
	if md.Spec.Template.Labels[v1alpha1.LabelClusterName] != "my-cluster" {
		t.Error("template labels missing cluster-name")
	}

	// OwnerRef
	if len(md.OwnerReferences) != 1 || md.OwnerReferences[0].Kind != "WorkerGroupClaim" {
		t.Errorf("ownerRefs = %v", md.OwnerReferences)
	}
}

func TestBuildMachineDeployment_WithTaints(t *testing.T) {
	claim := testClaim()
	claim.Spec.Taints = []v1alpha1.MachineTaint{
		{
			Key:         "dedicated",
			Value:       "gpu",
			Effect:      corev1.TaintEffectNoSchedule,
			Propagation: "Always",
		},
		{
			Key:         "special",
			Effect:      corev1.TaintEffectNoExecute,
			Propagation: "OnInitialization",
		},
	}

	md := BuildMachineDeployment(claim, claim.Spec.NodeLabels, "bmt", "kct")

	if len(md.Spec.Template.Spec.Taints) != 2 {
		t.Fatalf("taints len = %d, want 2", len(md.Spec.Template.Spec.Taints))
	}
	if md.Spec.Template.Spec.Taints[0].Key != "dedicated" {
		t.Errorf("taint[0].key = %s", md.Spec.Template.Spec.Taints[0].Key)
	}
	if string(md.Spec.Template.Spec.Taints[0].Propagation) != "Always" {
		t.Errorf("taint[0].propagation = %s", md.Spec.Template.Spec.Taints[0].Propagation)
	}
}

func TestBuildMachineDeployment_WithNodeLabels(t *testing.T) {
	claim := testClaim()
	claim.Spec.NodeLabels = map[string]string{
		"environment": "prod",
		"zone":        "eu-west-1",
	}

	md := BuildMachineDeployment(claim, claim.Spec.NodeLabels, "bmt", "kct")

	labels := md.Spec.Template.Labels
	if labels["environment"] != "prod" {
		t.Error("missing environment label in template")
	}
	if labels["zone"] != "eu-west-1" {
		t.Error("missing zone label in template")
	}
	// System labels still present
	if labels[v1alpha1.LabelClusterName] != "my-cluster" {
		t.Error("missing system cluster-name label")
	}
}

func TestBuildMachineDeployment_UsesPassedLabelsNotClaimSpec(t *testing.T) {
	claim := testClaim()
	claim.Spec.NodeLabels = map[string]string{
		"environment":                   "prod",
		"cluster.x-k8s.io/cluster-name": "hijacked",
	}
	sanitized := map[string]string{"environment": "prod"}

	md := BuildMachineDeployment(claim, sanitized, "bmt", "kct")

	if _, exists := md.Spec.Template.Labels["cluster.x-k8s.io/cluster-name"]; exists {
		if md.Spec.Template.Labels["cluster.x-k8s.io/cluster-name"] == "hijacked" {
			t.Error("dropped label leaked from claim.Spec.NodeLabels into the MD template")
		}
	}
	if md.Spec.Template.Labels["environment"] != "prod" {
		t.Error("missing environment label in template")
	}
}

func TestBuildMachineDeployment_SystemLabelsWin(t *testing.T) {
	claim := testClaim()
	hijack := map[string]string{
		v1alpha1.LabelClusterName:              "evil",
		v1alpha1.LabelClaimName:                "evil",
		clusterv1.MachineDeploymentUniqueLabel: "evil",
		clusterv1.MachineDeploymentNameLabel:   "evil",
		clusterv1.MachineSetNameLabel:          "evil",
		"app":                                  "nginx",
	}

	md := BuildMachineDeployment(claim, hijack, "bmt", "kct")

	labels := md.Spec.Template.Labels
	if labels[v1alpha1.LabelClusterName] != "my-cluster" {
		t.Errorf("cluster-name = %q, system label must win", labels[v1alpha1.LabelClusterName])
	}
	if labels[v1alpha1.LabelClaimName] != claim.Name {
		t.Errorf("claim-name = %q, system label must win", labels[v1alpha1.LabelClaimName])
	}
	for _, key := range []string{
		clusterv1.MachineDeploymentUniqueLabel,
		clusterv1.MachineDeploymentNameLabel,
		clusterv1.MachineSetNameLabel,
	} {
		if _, exists := labels[key]; exists {
			t.Errorf("CAPI-owned key %q must be stripped from the template", key)
		}
	}
	if labels["app"] != "nginx" {
		t.Error("regular user label must survive")
	}
}

func TestBuildMachineDeployment_WithStrategy(t *testing.T) {
	claim := testClaim()
	maxSurge := intstr.FromInt32(1)
	maxUnavail := intstr.FromString("25%")
	claim.Spec.Strategy = &v1alpha1.MachineDeploymentStrategy{
		Type: "RollingUpdate",
		RollingUpdate: &v1alpha1.RollingUpdateStrategy{
			MaxSurge:       &maxSurge,
			MaxUnavailable: &maxUnavail,
			DeletePolicy:   stringPtr("Oldest"),
		},
	}

	md := BuildMachineDeployment(claim, claim.Spec.NodeLabels, "bmt", "kct")

	if string(md.Spec.Rollout.Strategy.Type) != "RollingUpdate" {
		t.Errorf("strategy.type = %s", md.Spec.Rollout.Strategy.Type)
	}
	if md.Spec.Rollout.Strategy.RollingUpdate.MaxSurge.IntVal != 1 {
		t.Errorf("maxSurge = %v", md.Spec.Rollout.Strategy.RollingUpdate.MaxSurge)
	}
	if md.Spec.Rollout.Strategy.RollingUpdate.MaxUnavailable.StrVal != "25%" {
		t.Errorf("maxUnavailable = %v", md.Spec.Rollout.Strategy.RollingUpdate.MaxUnavailable)
	}
	if string(md.Spec.Deletion.Order) != "Oldest" {
		t.Errorf("deletion.order = %s", md.Spec.Deletion.Order)
	}
}

func TestBuildMachineDeployment_DeletionDefaults(t *testing.T) {
	claim := testClaim()
	// No spec.deletion — should use hardcoded defaults
	md := BuildMachineDeployment(claim, claim.Spec.NodeLabels, "bmt", "kct")

	if md.Spec.Template.Spec.Deletion.NodeDrainTimeoutSeconds == nil {
		t.Fatal("nodeDrainTimeoutSeconds is nil")
	}
	if *md.Spec.Template.Spec.Deletion.NodeDrainTimeoutSeconds != 60 {
		t.Errorf("nodeDrainTimeoutSeconds = %d, want 60", *md.Spec.Template.Spec.Deletion.NodeDrainTimeoutSeconds)
	}
	if md.Spec.Template.Spec.Deletion.NodeVolumeDetachTimeoutSeconds == nil {
		t.Fatal("nodeVolumeDetachTimeoutSeconds is nil")
	}
	if *md.Spec.Template.Spec.Deletion.NodeVolumeDetachTimeoutSeconds != 60 {
		t.Errorf("nodeVolumeDetachTimeoutSeconds = %d, want 60", *md.Spec.Template.Spec.Deletion.NodeVolumeDetachTimeoutSeconds)
	}
	if md.Spec.Template.Spec.Deletion.NodeDeletionTimeoutSeconds == nil {
		t.Fatal("nodeDeletionTimeoutSeconds is nil")
	}
	if *md.Spec.Template.Spec.Deletion.NodeDeletionTimeoutSeconds != 120 {
		t.Errorf("nodeDeletionTimeoutSeconds = %d, want 120", *md.Spec.Template.Spec.Deletion.NodeDeletionTimeoutSeconds)
	}
}

func TestBuildMachineDeployment_DeletionOverrides(t *testing.T) {
	claim := testClaim()
	drain := int32(300)
	claim.Spec.Deletion = &v1alpha1.MachineDeletionConfig{
		NodeDrainTimeoutSeconds: &drain,
	}

	md := BuildMachineDeployment(claim, claim.Spec.NodeLabels, "bmt", "kct")

	if *md.Spec.Template.Spec.Deletion.NodeDrainTimeoutSeconds != 300 {
		t.Errorf("nodeDrainTimeoutSeconds = %d, want 300", *md.Spec.Template.Spec.Deletion.NodeDrainTimeoutSeconds)
	}
	// Other fields keep defaults
	if *md.Spec.Template.Spec.Deletion.NodeVolumeDetachTimeoutSeconds != 60 {
		t.Errorf("nodeVolumeDetachTimeoutSeconds = %d, want 60 (default)", *md.Spec.Template.Spec.Deletion.NodeVolumeDetachTimeoutSeconds)
	}
	if *md.Spec.Template.Spec.Deletion.NodeDeletionTimeoutSeconds != 120 {
		t.Errorf("nodeDeletionTimeoutSeconds = %d, want 120 (default)", *md.Spec.Template.Spec.Deletion.NodeDeletionTimeoutSeconds)
	}
}

func TestBuildMachineDeployment_NilOptionals(t *testing.T) {
	claim := testClaim()
	// No taints, no strategy, no nodeLabels
	md := BuildMachineDeployment(claim, claim.Spec.NodeLabels, "bmt", "kct")

	if len(md.Spec.Template.Spec.Taints) != 0 {
		t.Errorf("taints should be empty, got %d", len(md.Spec.Template.Spec.Taints))
	}
}

func TestMachineDeploymentName(t *testing.T) {
	name := MachineDeploymentName("my-cluster", "pool-1")
	if name != "my-cluster-pool-1" {
		t.Errorf("name = %s, want my-cluster-pool-1", name)
	}
}
