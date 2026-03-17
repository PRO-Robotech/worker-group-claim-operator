package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

func TestIsOrphanTemplate_Referenced(t *testing.T) {
	referenced := map[string]bool{"bmt-abc": true}
	if isOrphanTemplate("bmt-abc", time.Now().Add(-20*time.Minute), referenced, 10*time.Minute, time.Now()) {
		t.Error("referenced template should not be orphan")
	}
}

func TestIsOrphanTemplate_TooYoung(t *testing.T) {
	referenced := map[string]bool{}
	if isOrphanTemplate("bmt-new", time.Now().Add(-5*time.Minute), referenced, 10*time.Minute, time.Now()) {
		t.Error("template younger than grace period should not be orphan")
	}
}

func TestIsOrphanTemplate_Orphan(t *testing.T) {
	referenced := map[string]bool{}
	if !isOrphanTemplate("bmt-old", time.Now().Add(-15*time.Minute), referenced, 10*time.Minute, time.Now()) {
		t.Error("unreferenced template older than grace period should be orphan")
	}
}

func TestIsOrphanTemplate_ExactGracePeriod(t *testing.T) {
	referenced := map[string]bool{}
	now := time.Now()
	createdAt := now.Add(-10 * time.Minute)
	if !isOrphanTemplate("bmt-exact", createdAt, referenced, 10*time.Minute, now) {
		t.Error("template at exact grace period boundary should be orphan")
	}
}

func TestFindReferencedTemplates_Empty(t *testing.T) {
	bmts, kcts := findReferencedTemplates(nil)
	if len(bmts) != 0 || len(kcts) != 0 {
		t.Error("empty claims should produce empty sets")
	}
}

func TestFindReferencedTemplates_CurrentTemplates(t *testing.T) {
	claims := []v1alpha1.WorkerGroupClaim{
		{
			Status: v1alpha1.WorkerGroupClaimStatus{
				CurrentTemplates: &v1alpha1.CurrentTemplates{
					BMT: "cluster-pool-bmt-abc123",
					KCT: "cluster-pool-kct-def456",
				},
			},
		},
	}
	bmts, kcts := findReferencedTemplates(claims)
	if !bmts["cluster-pool-bmt-abc123"] {
		t.Error("should reference BMT from currentTemplates")
	}
	if !kcts["cluster-pool-kct-def456"] {
		t.Error("should reference KCT from currentTemplates")
	}
}

func TestFindReferencedTemplates_PendingDeletion(t *testing.T) {
	claims := []v1alpha1.WorkerGroupClaim{
		{
			Status: v1alpha1.WorkerGroupClaimStatus{
				CurrentTemplates: &v1alpha1.CurrentTemplates{
					BMT: "current-bmt",
					KCT: "current-kct",
				},
				PendingDeletion: []v1alpha1.ResourceRef{
					{Kind: v1alpha1.KindBMT, Name: "old-bmt"},
					{Kind: v1alpha1.KindKCT, Name: "old-kct"},
				},
			},
		},
	}
	bmts, kcts := findReferencedTemplates(claims)
	if !bmts["current-bmt"] || !bmts["old-bmt"] {
		t.Error("should reference both current and pending BMTs")
	}
	if !kcts["current-kct"] || !kcts["old-kct"] {
		t.Error("should reference both current and pending KCTs")
	}
}

func TestFindReferencedTemplates_MultipleClaims(t *testing.T) {
	claims := []v1alpha1.WorkerGroupClaim{
		{
			Status: v1alpha1.WorkerGroupClaimStatus{
				CurrentTemplates: &v1alpha1.CurrentTemplates{BMT: "bmt-1", KCT: "kct-1"},
			},
		},
		{
			Status: v1alpha1.WorkerGroupClaimStatus{
				CurrentTemplates: &v1alpha1.CurrentTemplates{BMT: "bmt-2", KCT: "kct-2"},
			},
		},
		{
			// No current templates (e.g., provisioning failed)
			Status: v1alpha1.WorkerGroupClaimStatus{},
		},
	}
	bmts, kcts := findReferencedTemplates(claims)
	if len(bmts) != 2 || len(kcts) != 2 {
		t.Errorf("expected 2 BMTs and 2 KCTs, got %d and %d", len(bmts), len(kcts))
	}
	if !bmts["bmt-1"] || !bmts["bmt-2"] {
		t.Error("should reference BMTs from all claims")
	}
}

func TestNewLabeledBMT(t *testing.T) {
	bmt := newLabeledBMT("test-bmt", "default", "pool-1", "cluster-1", metav1.Now())
	if bmt.GetName() != "test-bmt" {
		t.Errorf("expected name test-bmt, got %s", bmt.GetName())
	}
	if bmt.GetLabels()[v1alpha1.LabelClaimName] != "pool-1" {
		t.Error("should have claim name label")
	}
}
