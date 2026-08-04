package nodelabels

import (
	"regexp"
	"slices"
)

// claimReservedPatterns protect the MachineDeployment selector and CAPI bookkeeping keys.
var claimReservedPatterns = []string{
	`^cluster\.x-k8s\.io/`,
	`^workergroup\.in-cloud\.io/`,
	`^node-group\.beget\.com/`,
	`^node\.cluster\.x-k8s\.io/`,
	`^node-restriction\.kubernetes\.io/`,
	`^machine-template-hash$`,
}

// nodeSafetyPatterns are system Node labels; node-role.kubernetes.io/ is deliberately absent.
var nodeSafetyPatterns = []string{
	`^kubernetes\.io/`,
	`^beta\.kubernetes\.io/`,
	`^node\.kubernetes\.io/`,
	`^topology\.kubernetes\.io/`,
	`^failure-domain\.beta\.kubernetes\.io/`,
	`^node-restriction\.kubernetes\.io/`,
	`^k8s\.io/`,
	`^cluster\.x-k8s\.io/`,
	`^node\.cluster\.x-k8s\.io/`,
	`^workergroup\.in-cloud\.io/`,
	`^[^/]*\.beget\.com/`,
	`^machine-template-hash$`,
}

// Policy decides which label keys are allowed.
type Policy struct {
	safety  []*regexp.Regexp
	deny    []*regexp.Regexp
	skipped []string
}

// NewNodePolicy guards what may be written onto customer Nodes.
func NewNodePolicy(denyPatterns []string) *Policy {
	return newPolicy(nodeSafetyPatterns, denyPatterns)
}

// NewClaimPolicy guards what may enter the MachineDeployment the operator builds.
func NewClaimPolicy(denyPatterns []string) *Policy {
	return newPolicy(claimReservedPatterns, denyPatterns)
}

// newPolicy compiles the floor plus the deny patterns; RE2-incompatible ones are skipped.
func newPolicy(basePatterns, denyPatterns []string) *Policy {
	p := &Policy{safety: make([]*regexp.Regexp, 0, len(basePatterns))}

	for _, pattern := range basePatterns {
		p.safety = append(p.safety, regexp.MustCompile(pattern))
	}

	for _, pattern := range denyPatterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			p.skipped = append(p.skipped, pattern)

			continue
		}
		p.deny = append(p.deny, compiled)
	}

	return p
}

// Denies reports whether the key is refused.
func (p *Policy) Denies(key string) bool {
	for _, re := range p.safety {
		if re.MatchString(key) {
			return true
		}
	}
	for _, re := range p.deny {
		if re.MatchString(key) {
			return true
		}
	}

	return false
}

// Removable reports whether the key must be taken off a Node. Safety-floor keys never qualify.
func (p *Policy) Removable(key string) bool {
	for _, re := range p.safety {
		if re.MatchString(key) {
			return false
		}
	}
	for _, re := range p.deny {
		if re.MatchString(key) {
			return true
		}
	}

	return false
}

// Filter splits labels into the allowed set and the sorted refused keys.
func (p *Policy) Filter(labels map[string]string) (map[string]string, []string) {
	if len(labels) == 0 {
		return nil, nil
	}

	allowed := make(map[string]string, len(labels))
	var refused []string

	for key, value := range labels {
		if p.Denies(key) {
			refused = append(refused, key)

			continue
		}
		allowed[key] = value
	}

	slices.Sort(refused)

	return allowed, refused
}

// SkippedPatterns returns deny patterns that failed to compile.
func (p *Policy) SkippedPatterns() []string {
	return p.skipped
}

// CompiledPatterns returns the number of deny patterns in force.
func (p *Policy) CompiledPatterns() int {
	return len(p.deny)
}
