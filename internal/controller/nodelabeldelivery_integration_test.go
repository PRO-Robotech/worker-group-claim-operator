package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

// stubClients hands the reconciler a pre-built client instead of dialling a real cluster.
type stubClients struct {
	client client.Client
}

func (s stubClients) GetOrCreate(string, []byte) (client.Client, error) {
	return s.client, nil
}

var _ = Describe("NodeLabelDelivery Controller", func() {
	const (
		clusterName = "delivery-cluster"
		mdName      = "delivery-cluster-pool"
		providerID  = "beget:///delivery-uid"
	)

	newTargetClient := func(node *corev1.Node) client.Client {
		s := clientgoscheme.Scheme

		return fake.NewClientBuilder().WithScheme(s).WithObjects(node).Build()
	}

	provision := func(ns string) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-kubeconfig", Namespace: ns},
			Data:       map[string][]byte{"value": []byte("stub")},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		md := &clusterv1.MachineDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: mdName, Namespace: ns},
			Spec: clusterv1.MachineDeploymentSpec{
				ClusterName: clusterName,
				Replicas:    int32Ptr(1),
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{
					v1alpha1.LabelClusterName: clusterName,
					v1alpha1.LabelClaimName:   "pool",
				}},
				Template: clusterv1.MachineTemplateSpec{
					ObjectMeta: clusterv1.ObjectMeta{Labels: map[string]string{
						v1alpha1.LabelClusterName: clusterName,
						v1alpha1.LabelClaimName:   "pool",
						"app":                     "nginx",
						"example.com/team":        "payments",
						"kubernetes.io/hostname":  "hijack",
					}},
					Spec: clusterv1.MachineSpec{
						ClusterName: clusterName,
						Version:     "v1.30.1",
						Bootstrap: clusterv1.Bootstrap{
							ConfigRef: clusterv1.ContractVersionedObjectReference{
								APIGroup: v1alpha1.KCTGroup,
								Kind:     v1alpha1.KindKCT,
								Name:     "kct",
							},
						},
						InfrastructureRef: clusterv1.ContractVersionedObjectReference{
							APIGroup: v1alpha1.BMTGroup,
							Kind:     v1alpha1.KindBMT,
							Name:     "bmt",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, md)).To(Succeed())

		machine := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mdName + "-abc",
				Namespace: ns,
				Labels:    map[string]string{clusterv1.MachineDeploymentNameLabel: mdName},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: clusterName,
				ProviderID:  providerID,
				Version:     "v1.30.1",
				Bootstrap: clusterv1.Bootstrap{
					ConfigRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: v1alpha1.KCTGroup, Kind: v1alpha1.KindKCT, Name: "kct",
					},
				},
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: v1alpha1.BMTGroup, Kind: v1alpha1.KindBMT, Name: "bmt",
				},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())

		machine.Status.NodeRef = clusterv1.MachineNodeReference{Name: "worker-1"}
		Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())
	}

	It("should deliver allowed labels and refuse safety-listed ones", func() {
		ns := createNamespace("test-delivery")
		provision(ns)

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-1",
				Labels: map[string]string{"node.longhorn.io/role": "storage"},
			},
			Spec: corev1.NodeSpec{ProviderID: providerID},
		}
		target := newTargetClient(node)

		r := &NodeLabelDeliveryReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
			Clients:  stubClients{client: target},
		}

		_, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: mdName, Namespace: ns},
		})
		Expect(err).NotTo(HaveOccurred())

		got := &corev1.Node{}
		Expect(target.Get(ctx, client.ObjectKey{Name: "worker-1"}, got)).To(Succeed())

		By("delivering the user labels")
		Expect(got.Labels).To(HaveKeyWithValue("app", "nginx"))
		Expect(got.Labels).To(HaveKeyWithValue("example.com/team", "payments"))

		By("refusing a safety-listed key even though it reached the MachineDeployment")
		Expect(got.Labels).NotTo(HaveKeyWithValue("kubernetes.io/hostname", "hijack"))

		By("leaving a hand-placed label alone")
		Expect(got.Labels).To(HaveKeyWithValue("node.longhorn.io/role", "storage"))

		By("not delivering the selector labels")
		Expect(got.Labels).NotTo(HaveKey(v1alpha1.LabelClaimName))
	})

	It("should not write anything in observe-only mode", func() {
		ns := createNamespace("test-delivery-observe")
		provision(ns)

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Spec:       corev1.NodeSpec{ProviderID: providerID},
		}
		target := newTargetClient(node)

		r := &NodeLabelDeliveryReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			Recorder:    record.NewFakeRecorder(10),
			Clients:     stubClients{client: target},
			ObserveOnly: true,
		}

		_, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: mdName, Namespace: ns},
		})
		Expect(err).NotTo(HaveOccurred())

		got := &corev1.Node{}
		Expect(target.Get(ctx, client.ObjectKey{Name: "worker-1"}, got)).To(Succeed())
		Expect(got.Labels).NotTo(HaveKey("app"))
	})

	It("should back off when the kubeconfig secret is missing", func() {
		ns := createNamespace("test-delivery-nosecret")

		md := &clusterv1.MachineDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: mdName, Namespace: ns},
			Spec: clusterv1.MachineDeploymentSpec{
				ClusterName: clusterName,
				Replicas:    int32Ptr(1),
				Selector:    metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}},
				Template: clusterv1.MachineTemplateSpec{
					ObjectMeta: clusterv1.ObjectMeta{Labels: map[string]string{"a": "b"}},
					Spec: clusterv1.MachineSpec{
						ClusterName: clusterName,
						Version:     "v1.30.1",
						Bootstrap: clusterv1.Bootstrap{
							ConfigRef: clusterv1.ContractVersionedObjectReference{
								APIGroup: v1alpha1.KCTGroup,
								Kind:     v1alpha1.KindKCT,
								Name:     "kct",
							},
						},
						InfrastructureRef: clusterv1.ContractVersionedObjectReference{
							APIGroup: v1alpha1.BMTGroup,
							Kind:     v1alpha1.KindBMT,
							Name:     "bmt",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, md)).To(Succeed())

		r := &NodeLabelDeliveryReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
			Clients:  stubClients{},
		}

		res, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: mdName, Namespace: ns},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(unreachableRequeue))
	})
})
