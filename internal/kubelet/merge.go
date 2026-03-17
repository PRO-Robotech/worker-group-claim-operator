package kubelet

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/yaml"
)

//go:embed defaults/kubelet-config.yaml
var defaultKubeletConfig []byte

// MergeKubeletConfig deep-merges user overrides into the default kubelet config.
// Returns the merged config as a YAML string suitable for injection into
// bootstrap vars as __kubeletConfigYaml.
// If overrides is nil or empty, returns the default config unchanged.
func MergeKubeletConfig(overrides map[string]any) (string, error) {
	var defaults map[string]any
	if err := yaml.Unmarshal(defaultKubeletConfig, &defaults); err != nil {
		return "", fmt.Errorf("failed to parse default kubelet config: %w", err)
	}

	if len(overrides) > 0 {
		deepMerge(defaults, overrides)
	}

	data, err := yaml.Marshal(defaults)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged kubelet config: %w", err)
	}

	return string(data), nil
}

// OverridesToMap converts KubeletConfigurationOverrides fields to map[string]any.
// Only non-nil fields are included.
func OverridesToMap(overridesJSON []byte) (map[string]any, error) {
	if len(overridesJSON) == 0 {
		return nil, nil //nolint:nilnil // nil map is a valid "no overrides" result
	}
	var m map[string]any
	if err := json.Unmarshal(overridesJSON, &m); err != nil {
		return nil, fmt.Errorf("failed to parse kubelet overrides: %w", err)
	}

	return m, nil
}

// deepMerge recursively merges src into dst. Nested maps are merged recursively;
// scalar values in src overwrite dst.
func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal

			continue
		}
		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dstVal.(map[string]any)
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)

			continue
		}
		dst[key] = srcVal
	}
}
