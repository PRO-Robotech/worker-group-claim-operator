package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/pointpu/worker-group-claim-operator/internal/nodelabels"
)

func targetScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}

	return s
}

func nodeWith(name, providerID string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec:       corev1.NodeSpec{ProviderID: providerID},
	}
}

func machineWith(providerID, nodeName string, labels map[string]string) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Labels: labels},
		Spec:       clusterv1.MachineSpec{ProviderID: providerID},
	}
	m.Status.NodeRef = clusterv1.MachineNodeReference{Name: nodeName}

	return m
}

func TestFindNodeByProviderID(t *testing.T) {
	ctx := context.Background()
	// Node name deliberately differs from nodeRef to prove providerID is primary.
	node := nodeWith("real-node", "beget:///abc", nil)
	c := fake.NewClientBuilder().WithScheme(targetScheme(t)).WithObjects(node).Build()

	got, err := findNode(ctx, c, machineWith("beget:///abc", "wrong-name", nil))
	if err != nil {
		t.Fatalf("findNode: %v", err)
	}
	if got.Name != "real-node" {
		t.Errorf("findNode() = %q, want real-node", got.Name)
	}
}

func TestFindNodeFallsBackToNodeRef(t *testing.T) {
	ctx := context.Background()
	node := nodeWith("by-ref", "", nil)
	c := fake.NewClientBuilder().WithScheme(targetScheme(t)).WithObjects(node).Build()

	got, err := findNode(ctx, c, machineWith("", "by-ref", nil))
	if err != nil {
		t.Fatalf("findNode: %v", err)
	}
	if got.Name != "by-ref" {
		t.Errorf("findNode() = %q, want by-ref", got.Name)
	}
}

func TestDeliverObserveOnlyDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	node := nodeWith("n1", "beget:///abc", map[string]string{"existing": "1"})
	c := fake.NewClientBuilder().WithScheme(targetScheme(t)).WithObjects(node).Build()

	r := &NodeLabelDeliveryReconciler{ObserveOnly: true}
	err := r.deliver(ctx, c, machineWith("beget:///abc", "n1", nil),
		map[string]string{"app": "nginx"}, nodelabels.NewNodePolicy(nil))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	got := &corev1.Node{}
	if err := c.Get(ctx, client.ObjectKey{Name: "n1"}, got); err != nil {
		t.Fatalf("get node: %v", err)
	}
	if _, exists := got.Labels["app"]; exists {
		t.Error("observe-only must not write labels to the node")
	}
}

func TestForeignDeniedFindsOnlyUnownedDeniedKeys(t *testing.T) {
	policy := nodelabels.NewNodePolicy(nil)

	machine := machineWith("beget:///abc", "n1", map[string]string{
		"node-role.kubernetes.io/role": "storage",
	})
	node := nodeWith("n1", "beget:///abc", map[string]string{
		// denied by the safety list and owned by nobody we track -> removable
		"kubernetes.io/hostname": "n1",
		// denied, but CAPI owns it via the Machine -> must be left to CAPI
		"node-role.kubernetes.io/role": "storage",
		// allowed and managed by us -> not foreign
		"app": "nginx",
		// allowed, planted by hand -> must be preserved
		"node.longhorn.io/role": "storage",
	})

	got := foreignDenied(policy, machine, node, map[string]string{"app": "nginx"})
	if len(got) != 1 || got[0] != "kubernetes.io/hostname" {
		t.Errorf("foreignDenied() = %v, want [kubernetes.io/hostname]", got)
	}
}

func TestRemoveForeignDeletesOnlyListedKeys(t *testing.T) {
	ctx := context.Background()
	node := nodeWith("n1", "beget:///abc", map[string]string{
		"kubernetes.io/hostname": "n1",
		"app":                    "nginx",
		"node.longhorn.io/role":  "storage",
	})
	c := fake.NewClientBuilder().WithScheme(targetScheme(t)).WithObjects(node).Build()

	r := &NodeLabelDeliveryReconciler{}
	if err := r.removeForeign(ctx, c, "n1", []string{"kubernetes.io/hostname"}); err != nil {
		t.Fatalf("removeForeign: %v", err)
	}

	got := &corev1.Node{}
	if err := c.Get(ctx, client.ObjectKey{Name: "n1"}, got); err != nil {
		t.Fatalf("get node: %v", err)
	}
	if _, exists := got.Labels["kubernetes.io/hostname"]; exists {
		t.Error("denied key must be removed")
	}
	if got.Labels["app"] != "nginx" || got.Labels["node.longhorn.io/role"] != "storage" {
		t.Errorf("unrelated labels must survive, got %v", got.Labels)
	}
}
