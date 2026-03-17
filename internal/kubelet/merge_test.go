package kubelet

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestMergeKubeletConfig_NoOverrides(t *testing.T) {
	result, err := MergeKubeletConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "maxPods: 250") {
		t.Error("default maxPods should be 250")
	}
	if !strings.Contains(result, "kind: KubeletConfiguration") {
		t.Error("should contain kind: KubeletConfiguration")
	}
}

func TestMergeKubeletConfig_EmptyOverrides(t *testing.T) {
	result, err := MergeKubeletConfig(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "maxPods: 250") {
		t.Error("default maxPods should remain 250 with empty overrides")
	}
}

func TestMergeKubeletConfig_SingleOverride(t *testing.T) {
	overrides := map[string]any{
		"maxPods": 110,
	}
	result, err := MergeKubeletConfig(overrides)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "maxPods: 110") {
		t.Errorf("maxPods should be overridden to 110, got: %s", result)
	}
	// Other defaults preserved
	if !strings.Contains(result, "shutdownGracePeriod: 15s") {
		t.Error("non-overridden fields should be preserved")
	}
}

func TestMergeKubeletConfig_NestedOverride(t *testing.T) {
	overrides := map[string]any{
		"evictionHard": map[string]any{
			"memory.available": "100Mi",
			"nodefs.available": "10%",
		},
	}
	result, err := MergeKubeletConfig(overrides)
	if err != nil {
		t.Fatal(err)
	}
	// Parse result to check nested values
	var m map[string]any
	if err := yaml.Unmarshal([]byte(result), &m); err != nil {
		t.Fatal(err)
	}
	eviction, ok := m["evictionHard"].(map[string]any)
	if !ok {
		t.Fatalf("evictionHard type = %T, want map", m["evictionHard"])
	}
	if eviction["memory.available"] != "100Mi" {
		t.Errorf("evictionHard.memory.available = %v, want 100Mi", eviction["memory.available"])
	}
}

func TestMergeKubeletConfig_ValidYAML(t *testing.T) {
	overrides := map[string]any{
		"maxPods":     110,
		"cpuCFSQuota": false,
	}
	result, err := MergeKubeletConfig(overrides)
	if err != nil {
		t.Fatal(err)
	}
	// Verify it's valid YAML
	var m map[string]any
	if err := yaml.Unmarshal([]byte(result), &m); err != nil {
		t.Errorf("result is not valid YAML: %v", err)
	}
}

func TestDeepMerge(t *testing.T) {
	dst := map[string]any{
		"a": "original",
		"b": map[string]any{
			"b1": "original",
			"b2": "original",
		},
	}
	src := map[string]any{
		"a": "overridden",
		"b": map[string]any{
			"b1": "overridden",
		},
		"c": "new",
	}
	deepMerge(dst, src)

	if dst["a"] != "overridden" {
		t.Errorf("a = %v, want overridden", dst["a"])
	}
	bMap := dst["b"].(map[string]any)
	if bMap["b1"] != "overridden" {
		t.Errorf("b.b1 = %v, want overridden", bMap["b1"])
	}
	if bMap["b2"] != "original" {
		t.Errorf("b.b2 = %v, want original (preserved)", bMap["b2"])
	}
	if dst["c"] != "new" {
		t.Errorf("c = %v, want new", dst["c"])
	}
}
