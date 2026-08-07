package controller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func TestUserLabelsSubtractsSelector(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Selector: metav1.LabelSelector{MatchLabels: map[string]string{
				"cluster.x-k8s.io/cluster-name":      "c1",
				"workergroup.in-cloud.io/claim-name": "pool",
			}},
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{Labels: map[string]string{
					"cluster.x-k8s.io/cluster-name":      "c1",
					"workergroup.in-cloud.io/claim-name": "pool",
					"app":                                "nginx",
					"example.com/team":                   "payments",
				}},
			},
		},
	}

	want := map[string]string{"app": "nginx", "example.com/team": "payments"}
	if got := userLabels(md); !reflect.DeepEqual(got, want) {
		t.Errorf("userLabels() = %v, want %v", got, want)
	}
}

func TestUserLabelsEmpty(t *testing.T) {
	if got := userLabels(&clusterv1.MachineDeployment{}); got != nil {
		t.Errorf("userLabels() = %v, want nil", got)
	}
}

func TestExcludeCAPIOwned(t *testing.T) {
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			// CAPI syncs this domain from the Machine itself.
			"node-role.kubernetes.io/role": "storage",
			"example.com/team":             "payments",
		}},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			clusterv1.LabelsFromMachineAnnotation: "node-role.kubernetes.io/role",
		}},
	}

	desired := map[string]string{
		"node-role.kubernetes.io/role": "storage",
		"example.com/team":             "payments",
		"app":                          "nginx",
	}

	want := map[string]string{"example.com/team": "payments", "app": "nginx"}
	if got := excludeCAPIOwned(desired, machine, node); !reflect.DeepEqual(got, want) {
		t.Errorf("excludeCAPIOwned() = %v, want %v", got, want)
	}
}

func TestExcludeCAPIOwnedKeepsKeyMissingFromMachine(t *testing.T) {
	// A node-role key that is NOT on the Machine is invisible to CAPI: it will neither be
	// re-added nor removed by CAPI, so only this controller can manage it.
	machine := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}}
	node := &corev1.Node{}

	desired := map[string]string{"node-role.kubernetes.io/leftover": "1"}
	if got := excludeCAPIOwned(desired, machine, node); !reflect.DeepEqual(got, desired) {
		t.Errorf("excludeCAPIOwned() = %v, want %v", got, desired)
	}
}

func TestExcludeCAPIOwnedEmpty(t *testing.T) {
	got := excludeCAPIOwned(nil, &clusterv1.Machine{}, &corev1.Node{})
	if got != nil {
		t.Errorf("excludeCAPIOwned(nil) = %v, want nil", got)
	}
}
