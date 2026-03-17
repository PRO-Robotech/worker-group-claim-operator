package builder

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func baseMD() *clusterv1.MachineDeployment {
	replicas := int32(3)

	return &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: &replicas,
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: clusterv1.MachineSpec{
					Version: "v1.30.1",
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						Name: "bmt-abc12345",
					},
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: clusterv1.ContractVersionedObjectReference{
							Name: "kct-deadbeef",
						},
					},
				},
			},
		},
	}
}

func TestComputeMDDiff_NoChanges(t *testing.T) {
	a := baseMD()
	b := baseMD()
	diff := ComputeMDDiff(a, b)
	if diff.RefsChanged || diff.InPlaceChanged {
		t.Error("expected no changes")
	}
	if diff.NeedsUpdate() {
		t.Error("NeedsUpdate should be false")
	}
}

func TestComputeMDDiff_RefChange_InfraOnly(t *testing.T) {
	desired := baseMD()
	existing := baseMD()
	desired.Spec.Template.Spec.InfrastructureRef.Name = "bmt-new-hash"

	diff := ComputeMDDiff(desired, existing)
	if !diff.RefsChanged {
		t.Error("infraRef changed, refsChanged should be true")
	}
	if diff.InPlaceChanged {
		t.Error("no in-place changes expected")
	}
}

func TestComputeMDDiff_RefChange_BootstrapOnly(t *testing.T) {
	desired := baseMD()
	existing := baseMD()
	desired.Spec.Template.Spec.Bootstrap.ConfigRef.Name = "kct-new-hash"

	diff := ComputeMDDiff(desired, existing)
	if !diff.RefsChanged {
		t.Error("configRef changed, refsChanged should be true")
	}
}

func TestComputeMDDiff_InPlaceOnly_Replicas(t *testing.T) {
	desired := baseMD()
	existing := baseMD()
	newReplicas := int32(5)
	desired.Spec.Replicas = &newReplicas

	diff := ComputeMDDiff(desired, existing)
	if diff.RefsChanged {
		t.Error("no ref changes expected")
	}
	if !diff.InPlaceChanged {
		t.Error("replicas changed, inPlaceChanged should be true")
	}
}

func TestComputeMDDiff_InPlaceOnly_Version(t *testing.T) {
	desired := baseMD()
	existing := baseMD()
	desired.Spec.Template.Spec.Version = "v1.31.0"

	diff := ComputeMDDiff(desired, existing)
	if diff.RefsChanged {
		t.Error("no ref changes expected")
	}
	if !diff.InPlaceChanged {
		t.Error("version changed, inPlaceChanged should be true")
	}
}

func TestComputeMDDiff_InPlaceOnly_Labels(t *testing.T) {
	desired := baseMD()
	existing := baseMD()
	desired.Spec.Template.Labels["zone"] = "eu-west-1"

	diff := ComputeMDDiff(desired, existing)
	if diff.RefsChanged {
		t.Error("no ref changes expected")
	}
	if !diff.InPlaceChanged {
		t.Error("labels changed, inPlaceChanged should be true")
	}
}

func TestComputeMDDiff_InPlaceOnly_Taints(t *testing.T) {
	desired := baseMD()
	existing := baseMD()
	desired.Spec.Template.Spec.Taints = []clusterv1.MachineTaint{
		{Key: "dedicated", Value: "gpu", Effect: "NoSchedule", Propagation: "Always"},
	}

	diff := ComputeMDDiff(desired, existing)
	if diff.RefsChanged {
		t.Error("no ref changes expected")
	}
	if !diff.InPlaceChanged {
		t.Error("taints changed, inPlaceChanged should be true")
	}
}

func TestComputeMDDiff_BothChanges(t *testing.T) {
	desired := baseMD()
	existing := baseMD()
	desired.Spec.Template.Spec.InfrastructureRef.Name = "bmt-new"
	newReplicas := int32(10)
	desired.Spec.Replicas = &newReplicas

	diff := ComputeMDDiff(desired, existing)
	if !diff.RefsChanged {
		t.Error("ref changed")
	}
	if !diff.InPlaceChanged {
		t.Error("replicas changed")
	}
	if !diff.NeedsUpdate() {
		t.Error("NeedsUpdate should be true")
	}
}
