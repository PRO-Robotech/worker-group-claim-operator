package nodelabels

import (
	"reflect"
	"testing"
)

func TestPolicyDeniesSafetyKeys(t *testing.T) {
	p := NewNodePolicy(nil)

	denied := []string{
		"kubernetes.io/hostname",
		"kubernetes.io/os",
		"beta.kubernetes.io/instance-type",
		"node.kubernetes.io/instance-type",
		"topology.kubernetes.io/zone",
		"failure-domain.beta.kubernetes.io/region",
		"node-restriction.kubernetes.io/protected",
		"k8s.io/anything",
		"cluster.x-k8s.io/cluster-name",
		"node.cluster.x-k8s.io/managed",
		"workergroup.in-cloud.io/claim-name",
		"node-group.beget.com/name",
		"machine-template-hash",
	}
	for _, key := range denied {
		if !p.Denies(key) {
			t.Errorf("Denies(%q) = false, want true", key)
		}
	}

	allowed := []string{
		"app",
		"example.com/team",
		"node-role.kubernetes.io/worker",
		"my-kubernetes.io.example.com/x",
		"machine-template-hash-suffix",
	}
	for _, key := range allowed {
		if p.Denies(key) {
			t.Errorf("Denies(%q) = true, want false", key)
		}
	}
}

func TestPolicyDeniesFromSpec(t *testing.T) {
	p := NewNodePolicy([]string{`^example\.com/`})

	if !p.Denies("example.com/team") {
		t.Error("spec pattern must deny example.com/team")
	}
	if p.Denies("other.com/team") {
		t.Error("spec pattern must not deny other.com/team")
	}
}

func TestPolicySkipsUncompilablePatterns(t *testing.T) {
	// PCRE constructs RE2 rejects: lookahead and a possessive quantifier.
	p := NewNodePolicy([]string{`^good\.com/`, `(?=lookahead)`, `.*+`, `(?i)case-insensitive-is-fine`})

	if got := p.CompiledPatterns(); got != 2 {
		t.Errorf("CompiledPatterns() = %d, want 2", got)
	}
	if got := p.SkippedPatterns(); !reflect.DeepEqual(got, []string{`(?=lookahead)`, `.*+`}) {
		t.Errorf("SkippedPatterns() = %v", got)
	}
	// Safety list must stay in force despite the bad patterns.
	if !p.Denies("kubernetes.io/hostname") {
		t.Error("safety list must survive an uncompilable spec pattern")
	}
	if !p.Denies("good.com/x") {
		t.Error("the compilable pattern must still apply")
	}
}

func TestPolicyFilter(t *testing.T) {
	p := NewNodePolicy([]string{`^denied\.io/`})

	allowed, refused := p.Filter(map[string]string{
		"app":                    "nginx",
		"example.com/team":       "payments",
		"kubernetes.io/hostname": "hijack",
		"denied.io/x":            "1",
	})

	wantAllowed := map[string]string{"app": "nginx", "example.com/team": "payments"}
	if !reflect.DeepEqual(allowed, wantAllowed) {
		t.Errorf("allowed = %v, want %v", allowed, wantAllowed)
	}
	if want := []string{"denied.io/x", "kubernetes.io/hostname"}; !reflect.DeepEqual(refused, want) {
		t.Errorf("refused = %v, want %v (sorted)", refused, want)
	}
}

func TestPolicyFilterEmpty(t *testing.T) {
	p := NewNodePolicy(nil)

	allowed, refused := p.Filter(nil)
	if allowed != nil || refused != nil {
		t.Errorf("Filter(nil) = %v, %v; want nil, nil", allowed, refused)
	}
}
