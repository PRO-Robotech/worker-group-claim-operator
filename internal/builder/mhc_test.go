package builder

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

func TestBuildMachineHealthCheck_DisabledYieldsNil(t *testing.T) {
	claim := testClaim()

	if got := BuildMachineHealthCheck(claim); got != nil {
		t.Fatalf("absent healthCheck block must mean disabled, got %v", got)
	}

	claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: false}
	if got := BuildMachineHealthCheck(claim); got != nil {
		t.Fatalf("enabled=false must yield nil, got %v", got)
	}
}

func TestBuildMachineHealthCheck_Enabled(t *testing.T) {
	claim := testClaim()
	claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: true}

	mhc := BuildMachineHealthCheck(claim)
	if mhc == nil {
		t.Fatal("enabled=true must yield an object")
	}

	if mhc.Name != "my-cluster-pool-1-mhc" {
		t.Errorf("name = %s, want my-cluster-pool-1-mhc", mhc.Name)
	}
	if mhc.Namespace != claim.Namespace {
		t.Errorf("namespace = %s, want %s", mhc.Namespace, claim.Namespace)
	}
	if mhc.Spec.ClusterName != claim.Spec.ClusterName {
		t.Errorf("clusterName = %s", mhc.Spec.ClusterName)
	}

	want := SelectorLabels(claim)
	if len(mhc.Spec.Selector.MatchLabels) != len(want) {
		t.Fatalf("selector = %v, want %v", mhc.Spec.Selector.MatchLabels, want)
	}
	for k, v := range want {
		if mhc.Spec.Selector.MatchLabels[k] != v {
			t.Errorf("selector[%s] = %q, want %q", k, mhc.Spec.Selector.MatchLabels[k], v)
		}
	}

	if len(mhc.OwnerReferences) != 1 || mhc.OwnerReferences[0].Name != claim.Name {
		t.Errorf("ownerReferences = %v", mhc.OwnerReferences)
	}
	if !*mhc.OwnerReferences[0].Controller {
		t.Error("owner reference must be a controller reference")
	}
}

func TestBuildMachineHealthCheck_ThresholdsAreExplicit(t *testing.T) {
	claim := testClaim()
	claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: true}

	mhc := BuildMachineHealthCheck(claim)

	if mhc.Spec.Checks.NodeStartupTimeoutSeconds == nil || *mhc.Spec.Checks.NodeStartupTimeoutSeconds != 360 {
		t.Errorf("nodeStartupTimeoutSeconds = %v, want 360", mhc.Spec.Checks.NodeStartupTimeoutSeconds)
	}
	threshold := mhc.Spec.Remediation.TriggerIf.UnhealthyLessThanOrEqualTo
	if threshold == nil {
		t.Fatal("unhealthyLessThanOrEqualTo is nil, want 1")
	}
	got, err := intstr.GetScaledValueFromIntOrPercent(threshold, 0, false)
	if err != nil {
		t.Fatalf("CAPI would reject unhealthyLessThanOrEqualTo=%v: %v", threshold, err)
	}
	if got != 1 {
		t.Errorf("unhealthyLessThanOrEqualTo = %d, want 1", got)
	}

	conds := mhc.Spec.Checks.UnhealthyNodeConditions
	if len(conds) != 2 {
		t.Fatalf("unhealthyNodeConditions = %d, want 2", len(conds))
	}
	seen := map[corev1.ConditionStatus]int32{}
	for _, c := range conds {
		if c.Type != corev1.NodeReady {
			t.Errorf("condition type = %s, want Ready", c.Type)
		}
		if c.TimeoutSeconds == nil {
			t.Fatalf("timeoutSeconds must be explicit for status %s", c.Status)
		}
		seen[c.Status] = *c.TimeoutSeconds
	}
	for _, s := range []corev1.ConditionStatus{corev1.ConditionFalse, corev1.ConditionUnknown} {
		if seen[s] != DefaultUnhealthyTimeoutSeconds {
			t.Errorf("timeout for Ready=%s is %d, want %d", s, seen[s], DefaultUnhealthyTimeoutSeconds)
		}
	}
}

func TestBuildMachineHealthCheck_TimeoutIsSameForAnyReplicaCount(t *testing.T) {
	claim := testClaim()
	claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: true}

	for _, replicas := range []int32{1, 2, 5} {
		r := replicas
		claim.Spec.Replicas = &r
		for _, c := range BuildMachineHealthCheck(claim).Spec.Checks.UnhealthyNodeConditions {
			if *c.TimeoutSeconds != DefaultUnhealthyTimeoutSeconds {
				t.Errorf("replicas=%d: timeout for Ready=%s is %d, want %d",
					r, c.Status, *c.TimeoutSeconds, DefaultUnhealthyTimeoutSeconds)
			}
		}
	}
}

func TestSetUnhealthyTimeoutSeconds(t *testing.T) {
	t.Cleanup(func() { SetUnhealthyTimeoutSeconds(DefaultUnhealthyTimeoutSeconds) })

	SetUnhealthyTimeoutSeconds(120)
	claim := testClaim()
	claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: true}

	for _, c := range BuildMachineHealthCheck(claim).Spec.Checks.UnhealthyNodeConditions {
		if *c.TimeoutSeconds != 120 {
			t.Errorf("timeout for Ready=%s is %d, want 120", c.Status, *c.TimeoutSeconds)
		}
	}
}

func TestBuildMachineDeployment_SetsRemediationMaxInFlight(t *testing.T) {
	md := BuildMachineDeployment(testClaim(), nil, "bmt", "kct")

	if md.Spec.Remediation.MaxInFlight == nil {
		t.Fatal("maxInFlight must be set: CAPI default is unlimited")
	}
	if md.Spec.Remediation.MaxInFlight.IntValue() != 1 {
		t.Errorf("maxInFlight = %v, want 1", md.Spec.Remediation.MaxInFlight)
	}
}
