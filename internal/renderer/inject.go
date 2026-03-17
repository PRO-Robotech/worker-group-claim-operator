package renderer

import (
	"fmt"
	"sort"
	"strings"
)

// InjectNodeLabels auto-injects a "nodeLabels" variable into vars
// as a sorted "k1=v1,k2=v2" string, unless already explicitly set.
// Does nothing if nodeLabels map is empty.
func InjectNodeLabels(vars map[string]any, nodeLabels map[string]string) {
	if len(nodeLabels) == 0 {
		return
	}
	if _, exists := vars["nodeLabels"]; exists {
		return
	}

	keys := make([]string, 0, len(nodeLabels))
	for k := range nodeLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, nodeLabels[k]))
	}

	vars["nodeLabels"] = strings.Join(parts, ",")
}

// InjectMachineDeploymentName auto-injects a "machineDeploymentName" variable
// as "{clusterName}-{claimName}", unless already explicitly set.
func InjectMachineDeploymentName(vars map[string]any, clusterName, claimName string) {
	if _, exists := vars["machineDeploymentName"]; exists {
		return
	}
	vars["machineDeploymentName"] = fmt.Sprintf("%s-%s", clusterName, claimName)
}
