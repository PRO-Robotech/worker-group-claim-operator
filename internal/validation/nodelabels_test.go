package validation

import (
	"strings"
	"testing"
)

func TestValidateNodeLabels_Valid(t *testing.T) {
	labels := map[string]string{
		"app":             "nginx",
		"environment":     "production",
		"custom.io/label": "value",
	}
	if err := ValidateNodeLabels(labels); err != nil {
		t.Errorf("expected no error for valid labels, got: %v", err)
	}
}

func TestValidateNodeLabels_Empty(t *testing.T) {
	if err := ValidateNodeLabels(nil); err != nil {
		t.Errorf("expected no error for nil labels, got: %v", err)
	}
	if err := ValidateNodeLabels(map[string]string{}); err != nil {
		t.Errorf("expected no error for empty labels, got: %v", err)
	}
}

func TestValidateNodeLabels_SingleReserved(t *testing.T) {
	labels := map[string]string{
		"cluster.x-k8s.io/cluster-name": "my-cluster",
	}
	err := ValidateNodeLabels(labels)
	if err == nil {
		t.Fatal("expected error for reserved prefix")
	}
	if !strings.Contains(err.Error(), "cluster.x-k8s.io/cluster-name") {
		t.Errorf("error should contain key name: %v", err)
	}
}

func TestValidateNodeLabels_MultipleReserved(t *testing.T) {
	labels := map[string]string{
		"cluster.x-k8s.io/cluster-name":      "val",
		"workergroup.in-cloud.io/claim-name": "val",
		"node-group.beget.com/name":          "val",
		"valid-label":                        "val",
	}
	err := ValidateNodeLabels(labels)
	if err == nil {
		t.Fatal("expected error for reserved prefixes")
	}
	// Should mention all three reserved keys
	if !strings.Contains(err.Error(), "cluster.x-k8s.io/cluster-name") {
		t.Errorf("error should contain cluster.x-k8s.io key: %v", err)
	}
	if !strings.Contains(err.Error(), "workergroup.in-cloud.io/claim-name") {
		t.Errorf("error should contain workergroup key: %v", err)
	}
	if !strings.Contains(err.Error(), "node-group.beget.com/name") {
		t.Errorf("error should contain node-group key: %v", err)
	}
}
