package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func int32p(i int32) *int32 { return &i }

func TestIsRolloutComplete_NilMD(t *testing.T) {
	if isRolloutComplete(nil) {
		t.Error("nil MD should not be complete")
	}
}

func TestIsRolloutComplete_NilReplicas(t *testing.T) {
	md := &clusterv1.MachineDeployment{}
	if isRolloutComplete(md) {
		t.Error("nil replicas should not be complete")
	}
}

func TestIsRolloutComplete_V1Beta2_Complete(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{
			Conditions: []metav1.Condition{
				{
					Type:   clusterv1.RollingOutCondition,
					Status: metav1.ConditionFalse,
				},
			},
			UpToDateReplicas: int32p(3),
		},
	}
	if !isRolloutComplete(md) {
		t.Error("v1beta2 complete MD should return true")
	}
}

func TestIsRolloutComplete_V1Beta2_Rolling(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{
			Conditions: []metav1.Condition{
				{
					Type:   clusterv1.RollingOutCondition,
					Status: metav1.ConditionTrue,
				},
			},
			UpToDateReplicas: int32p(1),
		},
	}
	if isRolloutComplete(md) {
		t.Error("rolling out MD should return false")
	}
}

func TestIsRolloutComplete_V1Beta2_PartialUpToDate(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{
			Conditions: []metav1.Condition{
				{
					Type:   clusterv1.RollingOutCondition,
					Status: metav1.ConditionFalse,
				},
			},
			UpToDateReplicas: int32p(2), // only 2 of 3
		},
	}
	if isRolloutComplete(md) {
		t.Error("partial up-to-date should return false")
	}
}

func TestIsRolloutComplete_Legacy_Complete(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{
			Replicas: int32p(3),
			Deprecated: &clusterv1.MachineDeploymentDeprecatedStatus{
				V1Beta1: &clusterv1.MachineDeploymentV1Beta1DeprecatedStatus{
					UpdatedReplicas:     3,
					UnavailableReplicas: 0,
				},
			},
		},
	}
	if !isRolloutComplete(md) {
		t.Error("legacy complete should return true")
	}
}

func TestIsRolloutComplete_Legacy_Rolling(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{
			Replicas: int32p(3),
			Deprecated: &clusterv1.MachineDeploymentDeprecatedStatus{
				V1Beta1: &clusterv1.MachineDeploymentV1Beta1DeprecatedStatus{
					UpdatedReplicas:     1,
					UnavailableReplicas: 2,
				},
			},
		},
	}
	if isRolloutComplete(md) {
		t.Error("legacy rolling should return false")
	}
}

func TestIsRolloutComplete_V1Beta2_NilUpToDate(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{
			Conditions: []metav1.Condition{
				{
					Type:   clusterv1.RollingOutCondition,
					Status: metav1.ConditionFalse,
				},
			},
			// UpToDateReplicas nil
		},
	}
	if isRolloutComplete(md) {
		t.Error("nil upToDateReplicas should return false")
	}
}

func TestIsRolloutComplete_EmptyStatus(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{},
	}
	if isRolloutComplete(md) {
		t.Error("empty status should not be complete")
	}
}

func TestIsRolloutComplete_NilDeprecated(t *testing.T) {
	md := &clusterv1.MachineDeployment{
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: int32p(3),
		},
		Status: clusterv1.MachineDeploymentStatus{
			Replicas: int32p(3),
			// No conditions, no deprecated — should return false
		},
	}
	if isRolloutComplete(md) {
		t.Error("no conditions and no deprecated status should not be complete")
	}
}
