package hash

import (
	"strings"
	"testing"
)

func TestComputeHash_Determinism(t *testing.T) {
	input := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"
	h1 := ComputeHash(input)
	h2 := ComputeHash(input)
	if h1 != h2 {
		t.Errorf("non-deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 8 {
		t.Errorf("hash length = %d, want 8", len(h1))
	}
}

func TestComputeHash_DifferentInput(t *testing.T) {
	h1 := ComputeHash("input-a")
	h2 := ComputeHash("input-b")
	if h1 == h2 {
		t.Errorf("different inputs produced same hash: %s", h1)
	}
}

func TestResourceName_Format(t *testing.T) {
	name := ResourceName("my-cluster", "worker-pool-1", TypeBMT, "abc12345")
	want := "my-cluster-worker-pool-1-bmt-abc12345"
	if name != want {
		t.Errorf("ResourceName = %q, want %q", name, want)
	}
}

func TestResourceName_KCT(t *testing.T) {
	name := ResourceName("cluster", "pool", TypeKCT, "deadbeef")
	want := "cluster-pool-kct-deadbeef"
	if name != want {
		t.Errorf("ResourceName = %q, want %q", name, want)
	}
}

func TestResourceName_MaxLength(t *testing.T) {
	long := strings.Repeat("a", 200)
	name := ResourceName(long, long, TypeBMT, "12345678")
	if len(name) > 253 {
		t.Errorf("name length = %d, exceeds 253", len(name))
	}
}
