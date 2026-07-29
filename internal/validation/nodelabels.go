package validation

import (
	"fmt"
	"sort"
	"strings"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

// Reserved label key prefixes that users cannot use in nodeLabels.
var reservedPrefixes = []string{
	"cluster.x-k8s.io/",
	"workergroup.in-cloud.io/",
	"node-group.beget.com/",
	"node.cluster.x-k8s.io/",
	"node-restriction.kubernetes.io/",
}

// SanitizeNodeLabels splits labels into the accepted set and the keys dropped for
// using a reserved prefix. Syntactically invalid keys or values are a hard error:
// the apiserver would reject the MachineSet, leaving the group without machines.
func SanitizeNodeLabels(labels map[string]string) (map[string]string, []string, error) {
	if len(labels) == 0 {
		return nil, nil, nil
	}

	accepted := make(map[string]string, len(labels))
	var rejected, invalid []string

	for key, value := range labels {
		if errs := k8svalidation.IsQualifiedName(key); len(errs) != 0 {
			invalid = append(invalid, fmt.Sprintf("%s: %s", key, strings.Join(errs, "; ")))

			continue
		}
		if errs := k8svalidation.IsValidLabelValue(value); len(errs) != 0 {
			invalid = append(invalid, fmt.Sprintf("%s=%s: %s", key, value, strings.Join(errs, "; ")))

			continue
		}
		if isReserved(key) {
			rejected = append(rejected, key)

			continue
		}
		accepted[key] = value
	}

	if len(invalid) != 0 {
		sort.Strings(invalid)

		return nil, nil, fmt.Errorf("nodeLabels are invalid: %v", invalid)
	}

	sort.Strings(rejected)

	return accepted, rejected, nil
}

func isReserved(key string) bool {
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}
