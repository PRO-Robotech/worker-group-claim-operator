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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/intstr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
	"github.com/pointpu/worker-group-claim-operator/internal/builder"
)

var _ = Describe("Autohealing: MachineHealthCheck lifecycle", func() {

	mhcKey := func(claimName, ns string) types.NamespacedName {
		return types.NamespacedName{
			Name:      builder.MachineHealthCheckName("my-cluster", claimName),
			Namespace: ns,
		}
	}

	getMHC := func(claimName, ns string) (*clusterv1.MachineHealthCheck, error) {
		mhc := &clusterv1.MachineHealthCheck{}
		err := k8sClient.Get(ctx, mhcKey(claimName, ns), mhc)

		return mhc, err
	}

	mhcExists := func(claimName, ns string) bool {
		_, err := getMHC(claimName, ns)

		return err == nil
	}

	It("does not create a MachineHealthCheck when the block is absent", func() {
		ns := createNamespace("test-mhc-absent")
		createTemplates(ns)

		claim := newClaim("claim-mhc-absent", ns, "my-cluster")
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())

		Eventually(func() bool {
			md := &clusterv1.MachineDeployment{}
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      builder.MachineDeploymentName("my-cluster", "claim-mhc-absent"),
				Namespace: ns,
			}, md) == nil
		}, timeout, interval).Should(BeTrue(), "MachineDeployment should exist")

		Consistently(func() bool {
			return mhcExists("claim-mhc-absent", ns)
		}, "2s", interval).Should(BeFalse(), "absent healthCheck block must mean disabled")
	})

	It("creates the object when enabled and removes it when switched off", func() {
		ns := createNamespace("test-mhc-toggle")
		createTemplates(ns)

		claim := newClaim("claim-mhc-toggle", ns, "my-cluster")
		claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: true}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())

		Eventually(func() bool {
			return mhcExists("claim-mhc-toggle", ns)
		}, timeout, interval).Should(BeTrue(), "enabling must create the MachineHealthCheck")

		mhc, err := getMHC("claim-mhc-toggle", ns)
		Expect(err).NotTo(HaveOccurred())
		Expect(mhc.Spec.ClusterName).To(Equal("my-cluster"))
		Expect(mhc.Spec.Selector.MatchLabels).To(Equal(builder.SelectorLabels(claim)))
		Expect(mhc.Spec.Checks.NodeStartupTimeoutSeconds).NotTo(BeNil())
		Expect(*mhc.Spec.Checks.NodeStartupTimeoutSeconds).To(BeEquivalentTo(360))
		Expect(mhc.Spec.Remediation.TriggerIf.UnhealthyLessThanOrEqualTo).NotTo(BeNil())
		Expect(intstr.GetScaledValueFromIntOrPercent(
			mhc.Spec.Remediation.TriggerIf.UnhealthyLessThanOrEqualTo, 0, false,
		)).To(Equal(1))

		Eventually(func() bool {
			fetched := &v1alpha1.WorkerGroupClaim{}
			if err := k8sClient.Get(ctx, claimKey("claim-mhc-toggle", ns), fetched); err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(fetched.Status.Conditions, v1alpha1.ConditionAutohealingConfigured)
		}, timeout, interval).Should(BeTrue(), "AutohealingConfigured should be True")

		Eventually(func() error {
			fetched := &v1alpha1.WorkerGroupClaim{}
			if err := k8sClient.Get(ctx, claimKey("claim-mhc-toggle", ns), fetched); err != nil {
				return err
			}
			fetched.Spec.HealthCheck.Enabled = false

			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(Succeed())

		Eventually(func() bool {
			return mhcExists("claim-mhc-toggle", ns)
		}, timeout, interval).Should(BeFalse(), "disabling must delete the MachineHealthCheck")

		Eventually(func() bool {
			fetched := &v1alpha1.WorkerGroupClaim{}
			if err := k8sClient.Get(ctx, claimKey("claim-mhc-toggle", ns), fetched); err != nil {
				return false
			}
			c := meta.FindStatusCondition(fetched.Status.Conditions, v1alpha1.ConditionAutohealingConfigured)

			return c != nil && c.Status == metav1.ConditionFalse && fetched.Status.HealthCheck == nil
		}, timeout, interval).Should(BeTrue(), "status should report autohealing as off")
	})

	It("uses a longer unhealthy timeout for a one-node group", func() {
		ns := createNamespace("test-mhc-single")
		createTemplates(ns)

		claim := newClaim("claim-mhc-single", ns, "my-cluster")
		claim.Spec.Replicas = int32Ptr(1)
		claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: true}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())

		Eventually(func() bool {
			return mhcExists("claim-mhc-single", ns)
		}, timeout, interval).Should(BeTrue())

		mhc, err := getMHC("claim-mhc-single", ns)
		Expect(err).NotTo(HaveOccurred())
		Expect(mhc.Spec.Checks.UnhealthyNodeConditions).To(HaveLen(2))
		for _, c := range mhc.Spec.Checks.UnhealthyNodeConditions {
			Expect(c.TimeoutSeconds).NotTo(BeNil())
			Expect(*c.TimeoutSeconds).To(BeEquivalentTo(900),
				"one-node group must wait longer: the unhealthy-count threshold is inert there")
		}
	})

	It("removes the object when the claim is deleted", func() {
		ns := createNamespace("test-mhc-delete")
		createTemplates(ns)

		claim := newClaim("claim-mhc-delete", ns, "my-cluster")
		claim.Spec.HealthCheck = &v1alpha1.WorkerGroupHealthCheck{Enabled: true}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())

		Eventually(func() bool {
			return mhcExists("claim-mhc-delete", ns)
		}, timeout, interval).Should(BeTrue())

		Expect(k8sClient.Delete(ctx, claim)).To(Succeed())

		Eventually(func() bool {
			_, err := getMHC("claim-mhc-delete", ns)

			return apierrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue(), "claim deletion must take the MachineHealthCheck with it")
	})
})
