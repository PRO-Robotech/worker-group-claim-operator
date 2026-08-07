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
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capilabels "sigs.k8s.io/cluster-api/util/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
	"github.com/pointpu/worker-group-claim-operator/internal/nodelabels"
	"github.com/pointpu/worker-group-claim-operator/internal/remote"
)

const (
	// FieldManager is part of the contract: renaming it orphans every applied label.
	FieldManager = "nodelabel-controller"

	// LabelsFromClaimAnnotation records the keys this controller put on a Node.
	LabelsFromClaimAnnotation = "nodelabels.in-cloud.io/labels-from-claim"

	kubeconfigSecretSuffix = "-kubeconfig"
	deliveryResyncInterval = 10 * time.Minute
	unreachableRequeue     = 15 * time.Minute
)

// NodeLabelDeliveryReconciler delivers user node labels to the Nodes of a target cluster.
type NodeLabelDeliveryReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Clients  ClientProvider

	// ObserveOnly reports the outcome without writing to target clusters.
	ObserveOnly bool

	// RemoveForeign strips policy-denied keys owned by another writer.
	RemoveForeign bool
}

// ClientProvider hands out a client for a target cluster given its kubeconfig.
type ClientProvider interface {
	GetOrCreate(cacheKey string, kubeconfig []byte) (client.Client, error)
}

// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machines,verbs=get;list;watch
// +kubebuilder:rbac:groups=workergroup.in-cloud.io,resources=nodelabelpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=workergroup.in-cloud.io,resources=nodelabelpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *NodeLabelDeliveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	md := &clusterv1.MachineDeployment{}
	if err := r.Get(ctx, req.NamespacedName, md); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	policy, err := r.loadPolicy(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("load policy: %w", err)
	}

	desired, refused := policy.Filter(userLabels(md))
	if len(refused) != 0 {
		log.Info("Labels refused by policy", "keys", refused)
	}

	remoteClient, err := r.targetClient(ctx, md)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Kubeconfig secret not found, backing off", "md", md.Name)

			return ctrl.Result{RequeueAfter: unreachableRequeue}, nil
		}

		return ctrl.Result{RequeueAfter: unreachableRequeue}, fmt.Errorf("target client: %w", err)
	}

	machines := &clusterv1.MachineList{}
	if err := r.List(ctx, machines,
		client.InNamespace(md.Namespace),
		client.MatchingLabels{clusterv1.MachineDeploymentNameLabel: md.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list machines: %w", err)
	}

	var delivered, total int
	for i := range machines.Items {
		machine := &machines.Items[i]
		if !machine.Status.NodeRef.IsDefined() {
			continue
		}
		total++

		if err := r.deliver(ctx, remoteClient, machine, desired, policy); err != nil {
			log.Error(err, "Failed to deliver node labels", "machine", machine.Name)

			continue
		}
		delivered++
	}

	log.V(1).Info("Delivery pass complete",
		"md", md.Name, "delivered", delivered, "total", total, "observeOnly", r.ObserveOnly)

	return ctrl.Result{RequeueAfter: deliveryResyncInterval}, nil
}

// userLabels returns the template labels minus the selector keys, which is the user set.
func userLabels(md *clusterv1.MachineDeployment) map[string]string {
	template := md.Spec.Template.Labels
	if len(template) == 0 {
		return nil
	}

	out := make(map[string]string, len(template))
	for key, value := range template {
		if _, isSelector := md.Spec.Selector.MatchLabels[key]; isSelector {
			continue
		}
		out[key] = value
	}

	return out
}

func (r *NodeLabelDeliveryReconciler) loadPolicy(ctx context.Context) (*nodelabels.Policy, error) {
	obj := &v1alpha1.NodeLabelPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: v1alpha1.NodeLabelPolicyName}, obj)
	if apierrors.IsNotFound(err) {
		return nodelabels.NewNodePolicy(nil), nil
	}
	if err != nil {
		return nil, err
	}

	policy := nodelabels.NewNodePolicy(obj.Spec.DenyPatterns)
	if err := r.reportPolicyStatus(ctx, obj, policy); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to report policy status")
	}

	return policy, nil
}

// reportPolicyStatus writes compiled/skipped counters, only when they change.
func (r *NodeLabelDeliveryReconciler) reportPolicyStatus(
	ctx context.Context, obj *v1alpha1.NodeLabelPolicy, policy *nodelabels.Policy,
) error {
	skipped := policy.SkippedPatterns()
	compiled := policy.CompiledPatterns()

	cond := metav1.Condition{
		Type:               v1alpha1.ConditionPatternsCompiled,
		Status:             metav1.ConditionTrue,
		Reason:             "AllPatternsCompiled",
		Message:            "All deny patterns compiled",
		ObservedGeneration: obj.Generation,
	}
	if len(skipped) != 0 {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "PatternsSkipped"
		cond.Message = fmt.Sprintf("Ignoring patterns unsupported by RE2: %s", strings.Join(skipped, ", "))
	}

	existing := meta.FindStatusCondition(obj.Status.Conditions, v1alpha1.ConditionPatternsCompiled)
	unchanged := obj.Status.CompiledPatterns == compiled &&
		slices.Equal(obj.Status.SkippedPatterns, skipped) &&
		existing != nil && existing.Status == cond.Status &&
		obj.Status.ObservedGeneration == obj.Generation
	if unchanged {
		return nil
	}

	obj.Status.CompiledPatterns = compiled
	obj.Status.SkippedPatterns = skipped
	obj.Status.ObservedGeneration = obj.Generation
	meta.SetStatusCondition(&obj.Status.Conditions, cond)

	return r.Status().Update(ctx, obj)
}

func (r *NodeLabelDeliveryReconciler) targetClient(
	ctx context.Context, md *clusterv1.MachineDeployment,
) (client.Client, error) {
	name := md.Spec.ClusterName + kubeconfigSecretSuffix
	key := types.NamespacedName{Name: name, Namespace: md.Namespace}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, key, secret); err != nil {
		return nil, err
	}

	kubeconfig, ok := secret.Data[remote.KubeconfigKey]
	if !ok {
		return nil, fmt.Errorf("secret %s has no %q key", key, remote.KubeconfigKey)
	}

	return r.Clients.GetOrCreate(key.String(), kubeconfig)
}

// deliver applies the managed labels to the Node backing the Machine.
func (r *NodeLabelDeliveryReconciler) deliver(
	ctx context.Context, remoteClient client.Client,
	machine *clusterv1.Machine, desired map[string]string, policy *nodelabels.Policy,
) error {
	node, err := findNode(ctx, remoteClient, machine)
	if err != nil {
		return err
	}

	managed := excludeCAPIOwned(desired, machine, node)
	foreign := foreignDenied(policy, machine, node, managed)

	if r.ObserveOnly {
		logf.FromContext(ctx).Info("Observe-only: would apply node labels",
			"node", node.Name, "keys", slices.Sorted(maps.Keys(managed)), "wouldRemove", foreign)

		return nil
	}

	if err := applyLabels(ctx, remoteClient, node.Name, managed); err != nil {
		return err
	}

	if r.RemoveForeign && len(foreign) != 0 {
		return r.removeForeign(ctx, remoteClient, node.Name, foreign)
	}

	return nil
}

// foreignDenied lists policy-denied Node keys owned neither by this controller nor by CAPI.
func foreignDenied(
	policy *nodelabels.Policy, machine *clusterv1.Machine,
	node *corev1.Node, managed map[string]string,
) []string {
	capiOwned := capilabels.GetManagedLabels(machine.Labels)

	var foreign []string
	for key := range node.Labels {
		if _, isManaged := managed[key]; isManaged {
			continue
		}
		if _, isCAPI := capiOwned[key]; isCAPI {
			continue
		}
		if policy.Removable(key) {
			foreign = append(foreign, key)
		}
	}
	slices.Sort(foreign)

	return foreign
}

// removeForeign deletes keys regardless of their field manager.
func (r *NodeLabelDeliveryReconciler) removeForeign(
	ctx context.Context, remoteClient client.Client, nodeName string, keys []string,
) error {
	labels := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		labels[key] = nil
	}

	patch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{"labels": labels},
	})
	if err != nil {
		return fmt.Errorf("build removal patch: %w", err)
	}

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	if err := remoteClient.Patch(ctx, node, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return fmt.Errorf("remove denied labels from node %s: %w", nodeName, err)
	}

	logf.FromContext(ctx).Info("Removed policy-denied labels owned by another writer",
		"node", nodeName, "keys", keys)

	return nil
}

// findNode resolves the Node by providerID, falling back to nodeRef.
func findNode(
	ctx context.Context, remoteClient client.Client, machine *clusterv1.Machine,
) (*corev1.Node, error) {
	if machine.Spec.ProviderID != "" {
		nodes := &corev1.NodeList{}
		if err := remoteClient.List(ctx, nodes); err != nil {
			return nil, fmt.Errorf("list nodes: %w", err)
		}
		for i := range nodes.Items {
			if nodes.Items[i].Spec.ProviderID == machine.Spec.ProviderID {
				return &nodes.Items[i], nil
			}
		}
	}

	node := &corev1.Node{}
	if err := remoteClient.Get(ctx, types.NamespacedName{Name: machine.Status.NodeRef.Name}, node); err != nil {
		return nil, fmt.Errorf("get node %s: %w", machine.Status.NodeRef.Name, err)
	}

	return node, nil
}

// excludeCAPIOwned drops the keys CAPI currently owns on this Node.
func excludeCAPIOwned(
	desired map[string]string, machine *clusterv1.Machine, node *corev1.Node,
) map[string]string {
	if len(desired) == 0 {
		return nil
	}

	owned := capilabels.GetManagedLabels(machine.Labels)
	for _, key := range strings.Split(node.Annotations[clusterv1.LabelsFromMachineAnnotation], ",") {
		if key != "" {
			owned[key] = ""
		}
	}

	out := make(map[string]string, len(desired))
	for key, value := range desired {
		if _, isCAPI := owned[key]; isCAPI {
			continue
		}
		out[key] = value
	}

	return out
}

// applyLabels server-side-applies the managed set without force.
func applyLabels(
	ctx context.Context, remoteClient client.Client, nodeName string, managed map[string]string,
) error {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Node",
		"metadata": map[string]interface{}{
			"name":        nodeName,
			"labels":      toStringMap(managed),
			"annotations": map[string]interface{}{LabelsFromClaimAnnotation: strings.Join(slices.Sorted(maps.Keys(managed)), ",")},
		},
	}}

	if err := remoteClient.Patch(ctx, obj, client.Apply, client.FieldOwner(FieldManager)); err != nil {
		return fmt.Errorf("apply labels to node %s: %w", nodeName, err)
	}

	return verifyOwnership(ctx, remoteClient, nodeName, managed)
}

// verifyOwnership reports co-owned keys, which SSA cannot remove by omission.
func verifyOwnership(
	ctx context.Context, remoteClient client.Client, nodeName string, managed map[string]string,
) error {
	if len(managed) == 0 {
		return nil
	}

	node := &corev1.Node{}
	if err := remoteClient.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return fmt.Errorf("read back node %s: %w", nodeName, err)
	}

	var coOwned []string
	for _, mf := range node.ManagedFields {
		if mf.Manager == FieldManager || mf.FieldsV1 == nil {
			continue
		}
		for key := range managed {
			if strings.Contains(string(mf.FieldsV1.Raw), fmt.Sprintf("f:%s", key)) {
				coOwned = append(coOwned, key)
			}
		}
	}

	if len(coOwned) != 0 {
		slices.Sort(coOwned)
		logf.FromContext(ctx).Info("Labels co-owned by another field manager, not removable by omission",
			"node", nodeName, "keys", coOwned)
	}

	return nil
}

func toStringMap(in map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func (r *NodeLabelDeliveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.MachineDeployment{}, builder.WithPredicates(templateLabelsChanged())).
		Watches(&clusterv1.Machine{}, handler.EnqueueRequestsFromMapFunc(machineToMachineDeployment)).
		Watches(&v1alpha1.NodeLabelPolicy{}, handler.EnqueueRequestsFromMapFunc(r.policyToMachineDeployments)).
		Named("nodelabeldelivery").
		Complete(r)
}

// templateLabelsChanged keeps the workqueue off status and replica updates.
func templateLabelsChanged() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldMD, okOld := e.ObjectOld.(*clusterv1.MachineDeployment)
			newMD, okNew := e.ObjectNew.(*clusterv1.MachineDeployment)
			if !okOld || !okNew {
				return false
			}

			return !maps.Equal(oldMD.Spec.Template.Labels, newMD.Spec.Template.Labels) ||
				!maps.Equal(oldMD.Spec.Selector.MatchLabels, newMD.Spec.Selector.MatchLabels)
		},
	}
}

// machineToMachineDeployment enqueues the owning MachineDeployment.
func machineToMachineDeployment(_ context.Context, obj client.Object) []reconcile.Request {
	mdName, ok := obj.GetLabels()[clusterv1.MachineDeploymentNameLabel]
	if !ok {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: mdName, Namespace: obj.GetNamespace()},
	}}
}

// policyToMachineDeployments re-reconciles everything on a policy change.
func (r *NodeLabelDeliveryReconciler) policyToMachineDeployments(
	ctx context.Context, _ client.Object,
) []reconcile.Request {
	mdList := &clusterv1.MachineDeploymentList{}
	if err := r.List(ctx, mdList); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list MachineDeployments for policy change")

		return nil
	}

	requests := make([]reconcile.Request, 0, len(mdList.Items))
	for i := range mdList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      mdList.Items[i].Name,
				Namespace: mdList.Items[i].Namespace,
			},
		})
	}

	return requests
}
