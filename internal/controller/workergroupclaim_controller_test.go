/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

const (
	timeout  = 10 * time.Second
	interval = 250 * time.Millisecond
)

var _ = Describe("WorkerGroupClaim Controller", func() {

	Context("Finalizer management (S-017)", func() {
		It("should add finalizer and set Provisioning phase on first reconcile", func() {
			ns := createNamespace("test-finalizer")

			createTemplates(ns)

			claim := newClaim("claim-finalizer", ns, "my-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for finalizer
			Eventually(func() bool {
				fetched := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("claim-finalizer", ns), fetched); err != nil {
					return false
				}

				return slices.Contains(fetched.Finalizers, v1alpha1.WorkerGroupClaimFinalizer)
			}, timeout, interval).Should(BeTrue(), "finalizer should be added")

			// Phase should be set (either Provisioning or Ready depending on reconcile speed)
			Eventually(func() string {
				fetched := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("claim-finalizer", ns), fetched); err != nil {
					return ""
				}

				return fetched.Status.Phase
			}, timeout, interval).ShouldNot(BeEmpty(), "phase should be set")
		})
	})

	Context("Initial provisioning (S-018, S-019)", func() {
		It("should create BMT, KCT, and MD from WorkerGroupClaim", func() {
			ns := createNamespace("test-provision")

			createTemplates(ns)

			claim := newClaim("pool-1", ns, "test-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for phase = Ready
			Eventually(func() string {
				fetched := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-1", ns), fetched); err != nil {
					return ""
				}

				return fetched.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Check status.currentTemplates is set
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-1", ns), fetched)).To(Succeed())
			Expect(fetched.Status.CurrentTemplates).NotTo(BeNil())
			Expect(fetched.Status.CurrentTemplates.BMT).To(ContainSubstring("test-cluster-pool-1-bmt-"))
			Expect(fetched.Status.CurrentTemplates.KCT).To(ContainSubstring("test-cluster-pool-1-kct-"))
			Expect(fetched.Status.LastRendered).NotTo(BeNil())
			Expect(fetched.Status.LastRendered.BMTHash).To(HaveLen(8))
			Expect(fetched.Status.LastRendered.KCTHash).To(HaveLen(8))

			// Verify BMT exists (unstructured)
			bmt := &unstructured.Unstructured{}
			bmt.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   v1alpha1.BMTGroup,
				Version: v1alpha1.BMTVersion,
				Kind:    v1alpha1.KindBMT,
			})
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fetched.Status.CurrentTemplates.BMT,
				Namespace: ns,
			}, bmt)).To(Succeed())
			Expect(bmt.GetLabels()[v1alpha1.LabelClusterName]).To(Equal("test-cluster"))
			Expect(bmt.GetLabels()[v1alpha1.LabelClaimName]).To(Equal("pool-1"))

			// Verify KCT exists (typed)
			kct := &bootstrapv1.KubeadmConfigTemplate{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fetched.Status.CurrentTemplates.KCT,
				Namespace: ns,
			}, kct)).To(Succeed())
			Expect(kct.Labels[v1alpha1.LabelClusterName]).To(Equal("test-cluster"))

			// Verify MD exists
			md := &clusterv1.MachineDeployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-cluster-pool-1",
				Namespace: ns,
			}, md)).To(Succeed())
			Expect(*md.Spec.Replicas).To(Equal(int32(3)))
			Expect(md.Spec.Template.Spec.Version).To(Equal("v1.30.1"))
			Expect(md.Spec.Template.Spec.InfrastructureRef.Name).To(Equal(fetched.Status.CurrentTemplates.BMT))
			Expect(md.Spec.Template.Spec.Bootstrap.ConfigRef.Name).To(Equal(fetched.Status.CurrentTemplates.KCT))
			Expect(md.Spec.ClusterName).To(Equal("test-cluster"))

			// Verify conditions
			readyCond := findCondition(fetched.Status.Conditions, v1alpha1.ConditionReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))

			renderedCond := findCondition(fetched.Status.Conditions, v1alpha1.ConditionTemplatesRendered)
			Expect(renderedCond).NotTo(BeNil())
			Expect(renderedCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set Failed phase when WGMachineTemplate not found", func() {
			ns := createNamespace("test-tmpl-missing")

			// Create only bootstrap template, NOT machine template
			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			claim := &v1alpha1.WorkerGroupClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-missing-tmpl",
					Namespace: ns,
				},
				Spec: v1alpha1.WorkerGroupClaimSpec{
					ClusterName:          "cluster-x",
					Replicas:             int32Ptr(1),
					Version:              "v1.30.1",
					MachineTemplateRef:   v1alpha1.TemplateRef{Name: "nonexistent-machine-tmpl"},
					BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Failed phase
			Eventually(func() string {
				fetched := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("claim-missing-tmpl", ns), fetched); err != nil {
					return ""
				}

				return fetched.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseFailed))

			// Check error in status
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("claim-missing-tmpl", ns), fetched)).To(Succeed())
			Expect(fetched.Status.LastRendered).NotTo(BeNil())
			Expect(fetched.Status.LastRendered.Error).To(ContainSubstring("nonexistent-machine-tmpl"))
		})

		It("should be idempotent — second reconcile should not create duplicates", func() {
			ns := createNamespace("test-idempotent")

			createTemplates(ns)

			claim := newClaim("pool-idem", ns, "idem-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				fetched := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-idem", ns), fetched); err != nil {
					return ""
				}

				return fetched.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Get current templates
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-idem", ns), fetched)).To(Succeed())
			bmtName := fetched.Status.CurrentTemplates.BMT
			kctName := fetched.Status.CurrentTemplates.KCT

			// Trigger another reconcile by updating a label
			fetched.Labels = map[string]string{"trigger": "requeue"}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait a bit and verify templates haven't changed
			time.Sleep(2 * time.Second)

			refetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-idem", ns), refetched)).To(Succeed())
			Expect(refetched.Status.CurrentTemplates.BMT).To(Equal(bmtName))
			Expect(refetched.Status.CurrentTemplates.KCT).To(Equal(kctName))
		})
	})

	Context("Watch strategy (S-020)", func() {
		It("should reconcile claim when WGMachineTemplate changes", func() {
			ns := createNamespace("test-watch")

			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			claim := newClaim("pool-watch", ns, "watch-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				fetched := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-watch", ns), fetched); err != nil {
					return ""
				}

				return fetched.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Record the current generation
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-watch", ns), fetched)).To(Succeed())
			originalBMT := fetched.Status.CurrentTemplates.BMT

			// Update the WGMachineTemplate (trigger watch)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "machine-tmpl-" + ns}, machineTmpl)).To(Succeed())
			machineTmpl.Labels = map[string]string{"updated": "true"}
			Expect(k8sClient.Update(ctx, machineTmpl)).To(Succeed())

			// The claim should be reconciled. Since template content didn't change,
			// BMT name should remain the same (hash unchanged)
			time.Sleep(2 * time.Second)
			Expect(k8sClient.Get(ctx, claimKey("pool-watch", ns), fetched)).To(Succeed())
			Expect(fetched.Status.CurrentTemplates.BMT).To(Equal(originalBMT))
		})
	})

	Context("Hash change detection (S-021)", func() {
		It("should detect hash change, start rollout, and move old to pendingDeletion", func() {
			ns := createNamespace("test-hash-change")

			// Create template with variables
			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContentWithVars()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			claim := &v1alpha1.WorkerGroupClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-hash", Namespace: ns},
				Spec: v1alpha1.WorkerGroupClaimSpec{
					ClusterName:          "hash-cluster",
					Replicas:             int32Ptr(2),
					Version:              "v1.30.1",
					MachineTemplateRef:   v1alpha1.TemplateRef{Name: "machine-tmpl-" + ns},
					BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
					Infrastructure: map[string]apiextensionsv1.JSON{
						"cpuCount": {Raw: []byte(`4`)},
						"memory":   {Raw: []byte(`8192`)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-hash", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Record old template names
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-hash", ns), fetched)).To(Succeed())
			oldBMT := fetched.Status.CurrentTemplates.BMT

			// Change infrastructure var → different hash
			fetched.Spec.Infrastructure["cpuCount"] = apiextensionsv1.JSON{Raw: []byte(`8`)}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for Updating phase
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-hash", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseUpdating))

			// Verify pendingDeletion contains old BMT
			fetched2 := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-hash", ns), fetched2)).To(Succeed())
			Expect(fetched2.Status.PendingDeletion).NotTo(BeEmpty())
			foundOldBMT := false
			for _, ref := range fetched2.Status.PendingDeletion {
				if ref.Kind == v1alpha1.KindBMT && ref.Name == oldBMT {
					foundOldBMT = true
				}
			}
			Expect(foundOldBMT).To(BeTrue(), "old BMT should be in pendingDeletion")

			// New BMT name differs from old
			Expect(fetched2.Status.CurrentTemplates.BMT).NotTo(Equal(oldBMT))

			// MD should reference new BMT
			md := &clusterv1.MachineDeployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "hash-cluster-pool-hash", Namespace: ns,
			}, md)).To(Succeed())
			Expect(md.Spec.Template.Spec.InfrastructureRef.Name).To(
				Equal(fetched2.Status.CurrentTemplates.BMT))

			// RolloutStartedAt should be set
			Expect(fetched2.Status.RolloutStartedAt).NotTo(BeNil())

			// RolloutComplete condition should be False
			rolloutCond := findCondition(fetched2.Status.Conditions, v1alpha1.ConditionRolloutComplete)
			Expect(rolloutCond).NotTo(BeNil())
			Expect(rolloutCond.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("Rollout tracking reconcile (S-023)", func() {
		It("should complete rollout and delete pendingDeletion when MD rollout finishes", func() {
			ns := createNamespace("test-rollout-complete")

			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContentWithVars()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			claim := &v1alpha1.WorkerGroupClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-rollout", Namespace: ns},
				Spec: v1alpha1.WorkerGroupClaimSpec{
					ClusterName:          "rollout-cluster",
					Replicas:             int32Ptr(2),
					Version:              "v1.30.1",
					MachineTemplateRef:   v1alpha1.TemplateRef{Name: "machine-tmpl-" + ns},
					BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
					Infrastructure: map[string]apiextensionsv1.JSON{
						"cpuCount": {Raw: []byte(`4`)},
						"memory":   {Raw: []byte(`8192`)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-rollout", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Record old BMT
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-rollout", ns), fetched)).To(Succeed())
			oldBMT := fetched.Status.CurrentTemplates.BMT

			// Trigger hash change
			fetched.Spec.Infrastructure["cpuCount"] = apiextensionsv1.JSON{Raw: []byte(`16`)}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for Updating phase
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-rollout", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseUpdating))

			// Simulate rollout completion by patching MD status
			md := &clusterv1.MachineDeployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "rollout-cluster-pool-rollout", Namespace: ns,
			}, md)).To(Succeed())
			desired := int32(2)
			md.Status.Replicas = &desired
			md.Status.ReadyReplicas = &desired
			md.Status.UpToDateReplicas = &desired
			md.Status.Conditions = []metav1.Condition{
				{
					Type:               clusterv1.RollingOutCondition,
					Status:             metav1.ConditionFalse,
					Reason:             "Complete",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, md)).To(Succeed())

			// Wait for Ready phase (rollout complete)
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-rollout", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Verify pendingDeletion is cleared
			fetched2 := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-rollout", ns), fetched2)).To(Succeed())
			Expect(fetched2.Status.PendingDeletion).To(BeEmpty())
			Expect(fetched2.Status.RolloutStartedAt).To(BeNil())

			// Verify old BMT was deleted
			oldBMTObj := &unstructured.Unstructured{}
			oldBMTObj.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   v1alpha1.BMTGroup,
				Version: v1alpha1.BMTVersion,
				Kind:    v1alpha1.KindBMT,
			})
			err := k8sClient.Get(ctx, types.NamespacedName{Name: oldBMT, Namespace: ns}, oldBMTObj)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "old BMT should be deleted after rollout complete")

			// Verify RolloutComplete condition
			rolloutCond := findCondition(fetched2.Status.Conditions, v1alpha1.ConditionRolloutComplete)
			Expect(rolloutCond).NotTo(BeNil())
			Expect(rolloutCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("Rollout timeout (S-024)", func() {
		It("should transition to Degraded when rollout times out", func() {
			ns := createNamespace("test-timeout")

			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContentWithVars()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			// Very short timeout for testing
			shortTimeout := metav1.Duration{Duration: 1 * time.Second}
			claim := &v1alpha1.WorkerGroupClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-timeout", Namespace: ns},
				Spec: v1alpha1.WorkerGroupClaimSpec{
					ClusterName:          "timeout-cluster",
					Replicas:             int32Ptr(2),
					Version:              "v1.30.1",
					MachineTemplateRef:   v1alpha1.TemplateRef{Name: "machine-tmpl-" + ns},
					BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
					RolloutTimeout:       &shortTimeout,
					Infrastructure: map[string]apiextensionsv1.JSON{
						"cpuCount": {Raw: []byte(`4`)},
						"memory":   {Raw: []byte(`8192`)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-timeout", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Trigger hash change
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-timeout", ns), fetched)).To(Succeed())
			fetched.Spec.Infrastructure["cpuCount"] = apiextensionsv1.JSON{Raw: []byte(`8`)}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for Updating first
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-timeout", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseUpdating))

			// Wait for Degraded (timeout is 1s, requeue is 30s, so this may take a bit)
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-timeout", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, 60*time.Second, interval).Should(Equal(v1alpha1.PhaseDegraded))

			// Verify RolloutTimedOut condition
			fetched2 := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-timeout", ns), fetched2)).To(Succeed())
			timedOutCond := findCondition(fetched2.Status.Conditions, v1alpha1.ConditionRolloutTimedOut)
			Expect(timedOutCond).NotTo(BeNil())
			Expect(timedOutCond.Status).To(Equal(metav1.ConditionTrue))

			// pendingDeletion should be preserved (NOT deleted)
			Expect(fetched2.Status.PendingDeletion).NotTo(BeEmpty())
		})
	})

	Context("In-place MD updates (S-025)", func() {
		It("should update replicas without creating new templates", func() {
			ns := createNamespace("test-inplace")

			createTemplates(ns)

			claim := newClaim("pool-inplace", ns, "inplace-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-inplace", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Record current templates
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-inplace", ns), fetched)).To(Succeed())
			bmtName := fetched.Status.CurrentTemplates.BMT
			kctName := fetched.Status.CurrentTemplates.KCT

			// Update replicas (in-place change, no new templates)
			fetched.Spec.Replicas = int32Ptr(5)
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for MD to be updated
			Eventually(func() int32 {
				md := &clusterv1.MachineDeployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "inplace-cluster-pool-inplace", Namespace: ns,
				}, md); err != nil {
					return 0
				}
				if md.Spec.Replicas == nil {
					return 0
				}

				return *md.Spec.Replicas
			}, timeout, interval).Should(Equal(int32(5)))

			// Verify templates did NOT change
			refetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-inplace", ns), refetched)).To(Succeed())
			Expect(refetched.Status.CurrentTemplates.BMT).To(Equal(bmtName))
			Expect(refetched.Status.CurrentTemplates.KCT).To(Equal(kctName))

			// Phase should still be Ready
			Expect(refetched.Status.Phase).To(Equal(v1alpha1.PhaseReady))

			// No pendingDeletion
			Expect(refetched.Status.PendingDeletion).To(BeEmpty())
		})
	})

	Context("Finalizer-based deletion (S-026)", func() {
		It("should delete MD, BMT, KCT in order and remove finalizer", func() {
			ns := createNamespace("test-deletion")

			createTemplates(ns)

			claim := newClaim("pool-delete", ns, "delete-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-delete", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Record template names
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-delete", ns), fetched)).To(Succeed())
			bmtName := fetched.Status.CurrentTemplates.BMT
			kctName := fetched.Status.CurrentTemplates.KCT

			// Delete the claim
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())

			// Wait for claim to be fully deleted (finalizer removed → GC)
			Eventually(func() bool {
				err := k8sClient.Get(ctx, claimKey("pool-delete", ns), &v1alpha1.WorkerGroupClaim{})

				return apierrors.IsNotFound(err)
			}, 30*time.Second, interval).Should(BeTrue(), "claim should be deleted after finalizer removal")

			// Verify MD was deleted
			md := &clusterv1.MachineDeployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "delete-cluster-pool-delete", Namespace: ns,
			}, md)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "MD should be deleted")

			// Verify BMT was deleted
			bmt := &unstructured.Unstructured{}
			bmt.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   v1alpha1.BMTGroup,
				Version: v1alpha1.BMTVersion,
				Kind:    v1alpha1.KindBMT,
			})
			err = k8sClient.Get(ctx, types.NamespacedName{Name: bmtName, Namespace: ns}, bmt)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "BMT should be deleted")

			// Verify KCT was deleted
			kct := &bootstrapv1.KubeadmConfigTemplate{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: kctName, Namespace: ns}, kct)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "KCT should be deleted")
		})
	})

	Context("Pause/resume (S-027)", func() {
		It("should not reconcile when paused (existing test)", func() {
			ns := createNamespace("test-pause")

			createTemplates(ns)

			claim := newClaim("pool-pause", ns, "pause-cluster")
			claim.Annotations = map[string]string{
				v1alpha1.PausedAnnotation: "true",
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait and verify MD is NOT created
			time.Sleep(3 * time.Second)

			md := &clusterv1.MachineDeployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "pause-cluster-pool-pause",
				Namespace: ns,
			}, md)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "MD should not exist when paused")
		})

		It("should set Paused phase and condition, then resume on unpause", func() {
			ns := createNamespace("test-pause-resume")

			createTemplates(ns)

			claim := newClaim("pool-pr", ns, "pr-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-pr", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Add pause annotation
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-pr", ns), fetched)).To(Succeed())
			fetched.Annotations = map[string]string{v1alpha1.PausedAnnotation: "true"}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for Paused phase
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-pr", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhasePaused))

			// Verify Paused condition
			fetched2 := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-pr", ns), fetched2)).To(Succeed())
			pausedCond := findCondition(fetched2.Status.Conditions, v1alpha1.ConditionPaused)
			Expect(pausedCond).NotTo(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionTrue))

			// Remove pause annotation → resume
			Expect(k8sClient.Get(ctx, claimKey("pool-pr", ns), fetched2)).To(Succeed())
			delete(fetched2.Annotations, v1alpha1.PausedAnnotation)
			Expect(k8sClient.Update(ctx, fetched2)).To(Succeed())

			// Wait for Ready phase (resumed)
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-pr", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Verify Paused condition cleared
			fetched3 := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-pr", ns), fetched3)).To(Succeed())
			pausedCond2 := findCondition(fetched3.Status.Conditions, v1alpha1.ConditionPaused)
			Expect(pausedCond2).NotTo(BeNil())
			Expect(pausedCond2.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("Failed recovery (S-028)", func() {
		It("should recover from Failed when template is created", func() {
			ns := createNamespace("test-recovery")

			// Create only bootstrap template, NOT machine template
			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			claim := &v1alpha1.WorkerGroupClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-recovery",
					Namespace: ns,
				},
				Spec: v1alpha1.WorkerGroupClaimSpec{
					ClusterName:          "recovery-cluster",
					Replicas:             int32Ptr(1),
					Version:              "v1.30.1",
					MachineTemplateRef:   v1alpha1.TemplateRef{Name: "machine-tmpl-" + ns},
					BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Failed phase
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("claim-recovery", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseFailed))

			// Verify currentTemplates NOT set (preserved nil from initial state)
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("claim-recovery", ns), fetched)).To(Succeed())
			Expect(fetched.Status.CurrentTemplates).To(BeNil(), "currentTemplates should not be set on failure")

			// Now create the missing machine template → recovery
			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			// Wait for Ready phase (recovery)
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("claim-recovery", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, 90*time.Second, interval).Should(Equal(v1alpha1.PhaseReady))

			// Verify templates and MD were created
			recovered := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("claim-recovery", ns), recovered)).To(Succeed())
			Expect(recovered.Status.CurrentTemplates).NotTo(BeNil())
			Expect(recovered.Status.LastRendered.Error).To(BeEmpty())
		})
	})

	Context("Full lifecycle E2E (S-031, S-032, S-034)", func() {
		It("should walk through complete lifecycle: provision → update → in-place → pause → resume → delete", func() {
			ns := createNamespace("test-e2e-lifecycle")

			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContentWithVars()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			// === Step 1: Create claim → Provisioning → Ready ===
			claim := &v1alpha1.WorkerGroupClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-e2e", Namespace: ns},
				Spec: v1alpha1.WorkerGroupClaimSpec{
					ClusterName:          "e2e-cluster",
					Replicas:             int32Ptr(2),
					Version:              "v1.30.1",
					MachineTemplateRef:   v1alpha1.TemplateRef{Name: "machine-tmpl-" + ns},
					BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
					Infrastructure: map[string]apiextensionsv1.JSON{
						"cpuCount": {Raw: []byte(`4`)},
						"memory":   {Raw: []byte(`8192`)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-e2e", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Verify all status fields after initial provisioning
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation),
				"observedGeneration should match metadata.generation")
			Expect(fetched.Status.CurrentTemplates).NotTo(BeNil())
			Expect(fetched.Status.CurrentTemplates.BMT).To(ContainSubstring("e2e-cluster-pool-e2e-bmt-"))
			Expect(fetched.Status.CurrentTemplates.KCT).To(ContainSubstring("e2e-cluster-pool-e2e-kct-"))
			Expect(fetched.Status.LastRendered).NotTo(BeNil())
			Expect(fetched.Status.LastRendered.BMTHash).To(HaveLen(8))
			Expect(fetched.Status.LastRendered.KCTHash).To(HaveLen(8))
			Expect(fetched.Status.LastRendered.Error).To(BeEmpty())
			Expect(fetched.Status.PendingDeletion).To(BeEmpty())
			Expect(fetched.Status.RolloutStartedAt).To(BeNil())
			// Conditions
			readyCond := findCondition(fetched.Status.Conditions, v1alpha1.ConditionReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			renderedCond := findCondition(fetched.Status.Conditions, v1alpha1.ConditionTemplatesRendered)
			Expect(renderedCond).NotTo(BeNil())
			Expect(renderedCond.Status).To(Equal(metav1.ConditionTrue))

			firstBMT := fetched.Status.CurrentTemplates.BMT

			// === Step 2: Update infra var → Updating (hash change) ===
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			fetched.Spec.Infrastructure["cpuCount"] = apiextensionsv1.JSON{Raw: []byte(`8`)}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-e2e", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseUpdating))

			// Verify status in Updating
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			Expect(fetched.Status.RolloutStartedAt).NotTo(BeNil())
			Expect(fetched.Status.CurrentTemplates.BMT).NotTo(Equal(firstBMT), "BMT should change after hash change")
			Expect(fetched.Status.PendingDeletion).NotTo(BeEmpty(), "old BMT should be in pendingDeletion")
			rolloutCond := findCondition(fetched.Status.Conditions, v1alpha1.ConditionRolloutComplete)
			Expect(rolloutCond).NotTo(BeNil())
			Expect(rolloutCond.Status).To(Equal(metav1.ConditionFalse))

			// Simulate rollout completion
			md := &clusterv1.MachineDeployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "e2e-cluster-pool-e2e", Namespace: ns,
			}, md)).To(Succeed())
			desired := int32(2)
			md.Status.Replicas = &desired
			md.Status.ReadyReplicas = &desired
			md.Status.UpToDateReplicas = &desired
			md.Status.Conditions = []metav1.Condition{
				{
					Type:               clusterv1.RollingOutCondition,
					Status:             metav1.ConditionFalse,
					Reason:             "Complete",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, md)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-e2e", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Verify cleanup after rollout
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			Expect(fetched.Status.PendingDeletion).To(BeEmpty())
			Expect(fetched.Status.RolloutStartedAt).To(BeNil())
			Expect(fetched.Status.ObservedGeneration).To(Equal(fetched.Generation))
			// MachineDeployment status mirrored
			Expect(fetched.Status.MachineDeployment).NotTo(BeNil())
			Expect(fetched.Status.MachineDeployment.Replicas).To(Equal(int32(2)))
			Expect(fetched.Status.MachineDeployment.ReadyReplicas).To(Equal(int32(2)))
			Expect(fetched.Status.MachineDeployment.UpToDateReplicas).To(Equal(int32(2)))

			secondBMT := fetched.Status.CurrentTemplates.BMT

			// === Step 3: In-place update — replicas ===
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			fetched.Spec.Replicas = int32Ptr(5)
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for MD replicas to be updated
			Eventually(func() int32 {
				md := &clusterv1.MachineDeployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "e2e-cluster-pool-e2e", Namespace: ns,
				}, md); err != nil || md.Spec.Replicas == nil {
					return 0
				}

				return *md.Spec.Replicas
			}, timeout, interval).Should(Equal(int32(5)))

			// BMT should NOT change
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			Expect(fetched.Status.CurrentTemplates.BMT).To(Equal(secondBMT))
			Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseReady))

			// === Step 4: In-place update — taints ===
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			fetched.Spec.Taints = []v1alpha1.MachineTaint{
				{
					Key:         "dedicated",
					Value:       "gpu",
					Effect:      corev1.TaintEffectNoSchedule,
					Propagation: "Always",
				},
			}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for MD taints to be updated
			Eventually(func() int {
				md := &clusterv1.MachineDeployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "e2e-cluster-pool-e2e", Namespace: ns,
				}, md); err != nil {
					return 0
				}

				return len(md.Spec.Template.Spec.Taints)
			}, timeout, interval).Should(Equal(1))

			// BMT should NOT change, no rollout
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			Expect(fetched.Status.CurrentTemplates.BMT).To(Equal(secondBMT))
			Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseReady))

			// === Step 5: Pause ===
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			fetched.Annotations = map[string]string{v1alpha1.PausedAnnotation: "true"}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-e2e", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhasePaused))

			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			pausedCond := findCondition(fetched.Status.Conditions, v1alpha1.ConditionPaused)
			Expect(pausedCond).NotTo(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionTrue))

			// === Step 6: Unpause → Resume ===
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			delete(fetched.Annotations, v1alpha1.PausedAnnotation)
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-e2e", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			pausedCond2 := findCondition(fetched.Status.Conditions, v1alpha1.ConditionPaused)
			Expect(pausedCond2).NotTo(BeNil())
			Expect(pausedCond2.Status).To(Equal(metav1.ConditionFalse))

			// === Step 7: Delete ===
			Expect(k8sClient.Get(ctx, claimKey("pool-e2e", ns), fetched)).To(Succeed())
			finalBMT := fetched.Status.CurrentTemplates.BMT
			finalKCT := fetched.Status.CurrentTemplates.KCT
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())

			// Wait for claim to be fully deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, claimKey("pool-e2e", ns), &v1alpha1.WorkerGroupClaim{})

				return apierrors.IsNotFound(err)
			}, 30*time.Second, interval).Should(BeTrue())

			// Verify all resources deleted
			md2 := &clusterv1.MachineDeployment{}
			Expect(apierrors.IsNotFound(
				k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-cluster-pool-e2e", Namespace: ns}, md2),
			)).To(BeTrue(), "MD should be deleted")

			bmtObj := &unstructured.Unstructured{}
			bmtObj.SetGroupVersionKind(schema.GroupVersionKind{
				Group: v1alpha1.BMTGroup, Version: v1alpha1.BMTVersion, Kind: v1alpha1.KindBMT,
			})
			Expect(apierrors.IsNotFound(
				k8sClient.Get(ctx, types.NamespacedName{Name: finalBMT, Namespace: ns}, bmtObj),
			)).To(BeTrue(), "BMT should be deleted")

			kctObj := &bootstrapv1.KubeadmConfigTemplate{}
			Expect(apierrors.IsNotFound(
				k8sClient.Get(ctx, types.NamespacedName{Name: finalKCT, Namespace: ns}, kctObj),
			)).To(BeTrue(), "KCT should be deleted")

			// Verify old BMT from Step 2 was also cleaned up (already deleted during rollout complete)
			Expect(apierrors.IsNotFound(
				k8sClient.Get(ctx, types.NamespacedName{Name: firstBMT, Namespace: ns}, bmtObj),
			)).To(BeTrue(), "first BMT should have been deleted during rollout cleanup")
		})
	})

	Context("Crash recovery (S-035)", func() {
		It("should resume rollout tracking after simulated restart", func() {
			ns := createNamespace("test-crash-recovery")

			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContentWithVars()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			claim := &v1alpha1.WorkerGroupClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-crash", Namespace: ns},
				Spec: v1alpha1.WorkerGroupClaimSpec{
					ClusterName:          "crash-cluster",
					Replicas:             int32Ptr(2),
					Version:              "v1.30.1",
					MachineTemplateRef:   v1alpha1.TemplateRef{Name: "machine-tmpl-" + ns},
					BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
					Infrastructure: map[string]apiextensionsv1.JSON{
						"cpuCount": {Raw: []byte(`4`)},
						"memory":   {Raw: []byte(`8192`)},
					},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-crash", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Trigger hash change
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-crash", ns), fetched)).To(Succeed())
			fetched.Spec.Infrastructure["cpuCount"] = apiextensionsv1.JSON{Raw: []byte(`8`)}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Wait for Updating
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-crash", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseUpdating))

			// Verify: pendingDeletion is set, rolloutStartedAt is set
			Expect(k8sClient.Get(ctx, claimKey("pool-crash", ns), fetched)).To(Succeed())
			Expect(fetched.Status.PendingDeletion).NotTo(BeEmpty())
			Expect(fetched.Status.RolloutStartedAt).NotTo(BeNil())

			// Simulate "crash recovery" by triggering a reconcile via label update.
			// The controller should re-enter Reconcile, detect pendingDeletion,
			// and continue tracking the rollout — no duplicate resources.
			Expect(k8sClient.Get(ctx, claimKey("pool-crash", ns), fetched)).To(Succeed())
			if fetched.Labels == nil {
				fetched.Labels = make(map[string]string)
			}
			fetched.Labels["crash-sim"] = "recovered"
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Should still be in Updating (rollout not complete yet)
			time.Sleep(2 * time.Second)
			Expect(k8sClient.Get(ctx, claimKey("pool-crash", ns), fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(v1alpha1.PhaseUpdating))
			// No duplicate BMTs — the controller re-used existing templates
			bmtList := &unstructured.UnstructuredList{}
			bmtList.SetGroupVersionKind(schema.GroupVersionKind{
				Group: v1alpha1.BMTGroup, Version: v1alpha1.BMTVersion, Kind: v1alpha1.KindBMTList,
			})
			Expect(k8sClient.List(ctx, bmtList,
				client.InNamespace(ns),
				client.HasLabels{v1alpha1.LabelClaimName},
			)).To(Succeed())
			// Should have exactly 2: old (in pendingDeletion) + current
			bmtCount := 0
			for _, item := range bmtList.Items {
				if item.GetLabels()[v1alpha1.LabelClaimName] == "pool-crash" {
					bmtCount++
				}
			}
			Expect(bmtCount).To(Equal(2), "should have exactly 2 BMTs (old + new), no duplicates")

			// Now complete the rollout
			md := &clusterv1.MachineDeployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "crash-cluster-pool-crash", Namespace: ns,
			}, md)).To(Succeed())
			desired := int32(2)
			md.Status.Replicas = &desired
			md.Status.ReadyReplicas = &desired
			md.Status.UpToDateReplicas = &desired
			md.Status.Conditions = []metav1.Condition{
				{
					Type:               clusterv1.RollingOutCondition,
					Status:             metav1.ConditionFalse,
					Reason:             "Complete",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, md)).To(Succeed())

			// Wait for Ready after recovery
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-crash", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Verify clean state
			Expect(k8sClient.Get(ctx, claimKey("pool-crash", ns), fetched)).To(Succeed())
			Expect(fetched.Status.PendingDeletion).To(BeEmpty())
			Expect(fetched.Status.RolloutStartedAt).To(BeNil())
		})
	})

	Context("Template fan-out (S-036)", func() {
		It("should reconcile all claims when shared template changes", func() {
			ns := createNamespace("test-fanout")

			// Shared templates
			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			// Create 3 claims referencing the same templates
			for i := 1; i <= 3; i++ {
				name := fmt.Sprintf("pool-fan-%d", i)
				cluster := fmt.Sprintf("fan-cluster-%d", i)
				c := newClaim(name, ns, cluster)
				Expect(k8sClient.Create(ctx, c)).To(Succeed())
			}

			// Wait for all 3 to be Ready
			for i := 1; i <= 3; i++ {
				name := fmt.Sprintf("pool-fan-%d", i)
				Eventually(func() string {
					f := &v1alpha1.WorkerGroupClaim{}
					if err := k8sClient.Get(ctx, claimKey(name, ns), f); err != nil {
						return ""
					}

					return f.Status.Phase
				}, timeout, interval).Should(Equal(v1alpha1.PhaseReady),
					"claim %s should be Ready", name)
			}

			// Record current KCTs for all 3 claims
			originalKCTs := map[string]string{}
			for i := 1; i <= 3; i++ {
				name := fmt.Sprintf("pool-fan-%d", i)
				f := &v1alpha1.WorkerGroupClaim{}
				Expect(k8sClient.Get(ctx, claimKey(name, ns), f)).To(Succeed())
				originalKCTs[name] = f.Status.CurrentTemplates.KCT
			}

			// Update the shared WGBootstrapTemplate content
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "bootstrap-tmpl-" + ns}, bootstrapTmpl)).To(Succeed())
			bootstrapTmpl.Spec.Value = `apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: KubeadmConfigTemplate
spec:
  template:
    spec:
      joinConfiguration:
        nodeRegistration:
          name: '{{ "{{ ds.meta_data.local_hostname }}" }}'
          kubeletExtraArgs:
            - name: "v"
              value: "4"`
			Expect(k8sClient.Update(ctx, bootstrapTmpl)).To(Succeed())

			// All 3 claims should get new KCTs (different hash due to content change)
			for i := 1; i <= 3; i++ {
				name := fmt.Sprintf("pool-fan-%d", i)
				Eventually(func() string {
					f := &v1alpha1.WorkerGroupClaim{}
					if err := k8sClient.Get(ctx, claimKey(name, ns), f); err != nil || f.Status.CurrentTemplates == nil {
						return ""
					}

					return f.Status.CurrentTemplates.KCT
				}, 20*time.Second, interval).ShouldNot(Equal(originalKCTs[name]),
					"claim %s should get new KCT after template update", name)
			}

			// Each claim should have its own unique KCT name
			newKCTs := map[string]string{}
			for i := 1; i <= 3; i++ {
				name := fmt.Sprintf("pool-fan-%d", i)
				f := &v1alpha1.WorkerGroupClaim{}
				Expect(k8sClient.Get(ctx, claimKey(name, ns), f)).To(Succeed())
				newKCTs[name] = f.Status.CurrentTemplates.KCT
			}
			// All KCTs should be different (different cluster names → different resource names)
			Expect(newKCTs["pool-fan-1"]).NotTo(Equal(newKCTs["pool-fan-2"]))
			Expect(newKCTs["pool-fan-2"]).NotTo(Equal(newKCTs["pool-fan-3"]))

			// Each claim should have independent pendingDeletion
			for i := 1; i <= 3; i++ {
				name := fmt.Sprintf("pool-fan-%d", i)
				f := &v1alpha1.WorkerGroupClaim{}
				Expect(k8sClient.Get(ctx, claimKey(name, ns), f)).To(Succeed())
				// Should either be in Updating (with pendingDeletion) or Ready (if rollout auto-completed)
				Expect(f.Status.Phase).To(Or(
					Equal(v1alpha1.PhaseUpdating),
					Equal(v1alpha1.PhaseReady),
				), "claim %s should be Updating or Ready", name)
			}
		})
	})

	Context("Orphan cleanup (S-029, S-030)", func() {
		It("should delete unreferenced BMT older than grace period", func() {
			ns := createNamespace("test-orphan")

			// Create an orphan BMT with operator labels (not referenced by any claim)
			orphanBMT := &unstructured.Unstructured{}
			orphanBMT.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   v1alpha1.BMTGroup,
				Version: v1alpha1.BMTVersion,
				Kind:    v1alpha1.KindBMT,
			})
			orphanBMT.SetName("orphan-bmt-test")
			orphanBMT.SetNamespace(ns)
			orphanBMT.SetLabels(map[string]string{
				v1alpha1.LabelClaimName:   "nonexistent-claim",
				v1alpha1.LabelClusterName: "nonexistent-cluster",
			})
			orphanBMT.Object["spec"] = map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"configuration": map[string]any{
							"cpuCount": int64(2),
							"memory":   int64(4096),
							"diskSize": int64(40960),
						},
						"image":             "ubuntu-22.04",
						"sshKeyIds":         []any{int64(123)},
						"usePrivateNetwork": true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, orphanBMT)).To(Succeed())

			// Also create an active claim with its own BMT (should NOT be deleted)
			createTemplates(ns)
			activeClaim := newClaim("pool-active", ns, "active-cluster")
			Expect(k8sClient.Create(ctx, activeClaim)).To(Succeed())

			// Wait for active claim to be Ready
			Eventually(func() string {
				f := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-active", ns), f); err != nil {
					return ""
				}

				return f.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			// Get active BMT name
			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-active", ns), fetched)).To(Succeed())
			activeBMTName := fetched.Status.CurrentTemplates.BMT

			// Run orphan cleanup with 0 grace period (for testing)
			cleaner := &OrphanCleaner{
				Client:      k8sClient,
				GracePeriod: 0, // no grace period for test
			}
			cleaner.CleanupOnce(ctx)

			// Verify orphan BMT was deleted
			checkBMT := &unstructured.Unstructured{}
			checkBMT.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   v1alpha1.BMTGroup,
				Version: v1alpha1.BMTVersion,
				Kind:    v1alpha1.KindBMT,
			})
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "orphan-bmt-test", Namespace: ns}, checkBMT)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "orphan BMT should be deleted")

			// Verify active BMT was NOT deleted
			activeBMT := &unstructured.Unstructured{}
			activeBMT.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   v1alpha1.BMTGroup,
				Version: v1alpha1.BMTVersion,
				Kind:    v1alpha1.KindBMT,
			})
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: activeBMTName, Namespace: ns}, activeBMT)).To(Succeed(),
				"active BMT should NOT be deleted")
		})
	})

	Context("Node labels rendering", func() {
		It("should reconcile to Ready when a template references .nodeLabels and the claim has none", func() {
			ns := createNamespace("test-empty-nodelabels")

			machineTmpl := &v1alpha1.WGMachineTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
				Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContent()},
			}
			Expect(k8sClient.Create(ctx, machineTmpl)).To(Succeed())

			bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
				Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContentWithNodeLabels()},
			}
			Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Succeed())

			claim := newClaim("pool-nolabels", ns, "nolabels-cluster")
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			Eventually(func() string {
				fetched := &v1alpha1.WorkerGroupClaim{}
				if err := k8sClient.Get(ctx, claimKey("pool-nolabels", ns), fetched); err != nil {
					return ""
				}

				return fetched.Status.Phase
			}, timeout, interval).Should(Equal(v1alpha1.PhaseReady))

			fetched := &v1alpha1.WorkerGroupClaim{}
			Expect(k8sClient.Get(ctx, claimKey("pool-nolabels", ns), fetched)).To(Succeed())
			renderedCond := findCondition(fetched.Status.Conditions, v1alpha1.ConditionTemplatesRendered)
			Expect(renderedCond).NotTo(BeNil())
			Expect(renderedCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})
})

// Helper functions

func createNamespace(name string) string {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	Expect(k8sClient.Create(ctx, ns)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))

	return name
}

func claimKey(name, ns string) types.NamespacedName {
	return types.NamespacedName{Name: name, Namespace: ns}
}

func int32Ptr(i int32) *int32 {
	return &i
}

func newClaim(name, ns, clusterName string) *v1alpha1.WorkerGroupClaim {
	return &v1alpha1.WorkerGroupClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: v1alpha1.WorkerGroupClaimSpec{
			ClusterName:          clusterName,
			Replicas:             int32Ptr(3),
			Version:              "v1.30.1",
			MachineTemplateRef:   v1alpha1.TemplateRef{Name: "machine-tmpl-" + ns},
			BootstrapTemplateRef: v1alpha1.TemplateRef{Name: "bootstrap-tmpl-" + ns},
		},
	}
}

func createTemplates(ns string) {
	machineTmpl := &v1alpha1.WGMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-tmpl-" + ns},
		Spec:       v1alpha1.WGMachineTemplateSpec{Value: bmtTemplateContent()},
	}
	Expect(k8sClient.Create(ctx, machineTmpl)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))

	bootstrapTmpl := &v1alpha1.WGBootstrapTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-tmpl-" + ns},
		Spec:       v1alpha1.WGBootstrapTemplateSpec{Value: kctTemplateContent()},
	}
	Expect(k8sClient.Create(ctx, bootstrapTmpl)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))
}

func bmtTemplateContentWithVars() string {
	return `apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: BegetMachineTemplate
spec:
  template:
    spec:
      configuration:
        cpuCount: {{ .cpuCount }}
        memory: {{ .memory }}
        diskSize: 61440
      image: "ubuntu-22.04"
      sshKeyIds:
        - 123
      usePrivateNetwork: true`
}

func bmtTemplateContent() string {
	return `apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: BegetMachineTemplate
spec:
  template:
    spec:
      configuration:
        cpuCount: 4
        memory: 8192
        diskSize: 61440
      image: "ubuntu-22.04"
      sshKeyIds:
        - 123
      usePrivateNetwork: true`
}

func kctTemplateContent() string {
	return `apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: KubeadmConfigTemplate
spec:
  template:
    spec:
      joinConfiguration:
        nodeRegistration:
          name: '{{ "{{ ds.meta_data.local_hostname }}" }}'`
}

func kctTemplateContentWithNodeLabels() string {
	return `apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: KubeadmConfigTemplate
spec:
  template:
    spec:
      joinConfiguration:
        nodeRegistration:
          kubeletExtraArgs:
            - name: node-labels
              value: "{{ .nodeLabels }}"
          name: '{{ "{{ ds.meta_data.local_hostname }}" }}'`
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}

	return nil
}
