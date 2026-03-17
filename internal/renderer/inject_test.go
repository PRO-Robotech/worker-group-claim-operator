package renderer

import (
	"testing"
)

func TestInjectNodeLabels_SortedOrder(t *testing.T) {
	vars := make(map[string]any)
	labels := map[string]string{
		"zone":        "eu-west-1",
		"environment": "prod",
		"app":         "nginx",
	}
	InjectNodeLabels(vars, labels)

	got, ok := vars["nodeLabels"].(string)
	if !ok {
		t.Fatalf("nodeLabels type = %T, want string", vars["nodeLabels"])
	}
	want := "app=nginx,environment=prod,zone=eu-west-1"
	if got != want {
		t.Errorf("nodeLabels = %q, want %q", got, want)
	}
}

func TestInjectNodeLabels_NoOverwrite(t *testing.T) {
	vars := map[string]any{
		"nodeLabels": "explicit-value",
	}
	labels := map[string]string{"key": "val"}
	InjectNodeLabels(vars, labels)

	if vars["nodeLabels"] != "explicit-value" {
		t.Errorf("nodeLabels was overwritten: %v", vars["nodeLabels"])
	}
}

func TestInjectNodeLabels_Empty(t *testing.T) {
	vars := make(map[string]any)
	InjectNodeLabels(vars, nil)
	if _, exists := vars["nodeLabels"]; exists {
		t.Error("nodeLabels should not be injected for nil labels")
	}

	InjectNodeLabels(vars, map[string]string{})
	if _, exists := vars["nodeLabels"]; exists {
		t.Error("nodeLabels should not be injected for empty labels")
	}
}

func TestInjectMachineDeploymentName(t *testing.T) {
	vars := make(map[string]any)
	InjectMachineDeploymentName(vars, "my-cluster", "worker-pool-1")

	got, ok := vars["machineDeploymentName"].(string)
	if !ok {
		t.Fatalf("machineDeploymentName type = %T, want string", vars["machineDeploymentName"])
	}
	if got != "my-cluster-worker-pool-1" {
		t.Errorf("machineDeploymentName = %q, want %q", got, "my-cluster-worker-pool-1")
	}
}

func TestInjectMachineDeploymentName_NoOverwrite(t *testing.T) {
	vars := map[string]any{
		"machineDeploymentName": "explicit-name",
	}
	InjectMachineDeploymentName(vars, "cluster", "pool")

	if vars["machineDeploymentName"] != "explicit-name" {
		t.Errorf("machineDeploymentName was overwritten: %v", vars["machineDeploymentName"])
	}
}
