package validation

import (
	"fmt"
	"strings"
)

// Reserved label key prefixes that users cannot use in nodeLabels.
var reservedPrefixes = []string{
	"cluster.x-k8s.io/",
	"workergroup.in-cloud.io/",
	"node-group.beget.com/",
}

// ValidateNodeLabels checks that none of the label keys use reserved prefixes.
// Returns an error listing ALL invalid keys (not just the first).
func ValidateNodeLabels(labels map[string]string) error {
	var invalid []string
	for key := range labels {
		for _, prefix := range reservedPrefixes {
			if strings.HasPrefix(key, prefix) {
				invalid = append(invalid, key)

				break
			}
		}
	}
	if len(invalid) != 0 {
		return fmt.Errorf("nodeLabels contain reserved prefixes: %v", invalid)
	}

	return nil
}
