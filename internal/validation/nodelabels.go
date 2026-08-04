package validation

import (
	"fmt"
	"slices"
	"strings"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	"github.com/pointpu/worker-group-claim-operator/internal/nodelabels"
)

// SanitizeNodeLabels splits labels into the accepted set and the keys dropped by policy.
func SanitizeNodeLabels(
	labels map[string]string, policy *nodelabels.Policy,
) (map[string]string, []string, error) {
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
		if policy.Denies(key) {
			rejected = append(rejected, key)

			continue
		}
		accepted[key] = value
	}

	if len(invalid) != 0 {
		slices.Sort(invalid)

		return nil, nil, fmt.Errorf("nodeLabels are invalid: %v", invalid)
	}

	slices.Sort(rejected)

	return accepted, rejected, nil
}
