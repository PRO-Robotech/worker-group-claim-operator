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
	"strings"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	builderpkg "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
	"github.com/pointpu/worker-group-claim-operator/internal/builder"
	"github.com/pointpu/worker-group-claim-operator/internal/hash"
	"github.com/pointpu/worker-group-claim-operator/internal/kubelet"
	"github.com/pointpu/worker-group-claim-operator/internal/nodelabels"
	"github.com/pointpu/worker-group-claim-operator/internal/renderer"
	"github.com/pointpu/worker-group-claim-operator/internal/validation"
)

const (
	defaultRolloutTimeout = 30 * time.Minute
	rolloutRequeueDelay   = 30 * time.Second
	degradedRequeueDelay  = 60 * time.Second
	failedRequeueDelay    = 60 * time.Second
	deleteRequeueDelay    = 10 * time.Second
)

// WorkerGroupClaimReconciler reconciles a WorkerGroupClaim object.
type WorkerGroupClaimReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=workergroup.in-cloud.io,resources=workergroupclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=workergroup.in-cloud.io,resources=workergroupclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=workergroup.in-cloud.io,resources=workergroupclaims/finalizers,verbs=update
// +kubebuilder:rbac:groups=workergroup.in-cloud.io,resources=wgbootstraptemplates;wgmachinetemplates;nodelabelpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=begetmachinetemplates,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kubeadmconfigtemplates,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinesets,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinehealthchecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinehealthchecks/status,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *WorkerGroupClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	claim := &v1alpha1.WorkerGroupClaim{}
	if err := r.Get(ctx, req.NamespacedName, claim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !claim.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, claim)
	}

	if isPaused(claim) {
		return r.reconcilePaused(ctx, claim)
	}

	if claim.Status.Phase == v1alpha1.PhasePaused {
		meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ConditionPaused,
			Status:             metav1.ConditionFalse,
			Reason:             "Resumed",
			Message:            "Reconciliation resumed",
			ObservedGeneration: claim.Generation,
		})
		r.Recorder.Event(claim, "Normal", "Resumed", "Reconciliation resumed")
	}

	if err := r.ensureFinalizer(ctx, claim); err != nil {
		return ctrl.Result{}, err
	}

	nodeLabels, rejectedLabels, err := validation.SanitizeNodeLabels(claim.Spec.NodeLabels, r.labelPolicy(ctx))
	if err != nil {
		return r.setFailed(ctx, claim, "NodeLabelsInvalid", err.Error())
	}
	r.setNodeLabelsAcceptedCondition(claim, rejectedLabels)

	bmtYAML, kctYAML, err := r.renderTemplates(ctx, claim, nodeLabels)
	if err != nil {
		return r.setFailed(ctx, claim, "RenderError", err.Error())
	}

	bmtHash := hash.ComputeHash(bmtYAML)
	bmtName := hash.ResourceName(claim.Spec.ClusterName, claim.Name, hash.TypeBMT, bmtHash)
	kctHash := hash.ComputeHash(kctYAML)
	kctName := hash.ResourceName(claim.Spec.ClusterName, claim.Name, hash.TypeKCT, kctHash)

	isFirstProvision := claim.Status.CurrentTemplates == nil
	var oldBMT, oldKCT string
	if !isFirstProvision {
		oldBMT = claim.Status.CurrentTemplates.BMT
		oldKCT = claim.Status.CurrentTemplates.KCT
	}
	hashChanged := !isFirstProvision && (oldBMT != bmtName || oldKCT != kctName)

	bmtCreated, err := r.ensureBMT(ctx, claim, bmtYAML, bmtName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure BMT: %w", err)
	}
	if bmtCreated {
		r.Recorder.Eventf(claim, "Normal", "InfrastructureTemplateCreated",
			"Created BegetMachineTemplate %s", bmtName)
	}

	kctCreated, kctUpdated, err := r.ensureKCT(ctx, claim, kctYAML, kctName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure KCT: %w", err)
	}
	if kctCreated {
		r.Recorder.Eventf(claim, "Normal", "BootstrapTemplateCreated",
			"Created KubeadmConfigTemplate %s", kctName)
	}
	if kctUpdated {
		r.Recorder.Eventf(claim, "Normal", "BootstrapTemplateUpdated",
			"Updated KubeadmConfigTemplate %s in-place", kctName)
	}

	if hashChanged {
		if oldBMT != bmtName {
			claim.Status.PendingDeletion = appendResourceRef(
				claim.Status.PendingDeletion, v1alpha1.KindBMT, oldBMT)
		}
		if oldKCT != kctName {
			claim.Status.PendingDeletion = appendResourceRef(
				claim.Status.PendingDeletion, v1alpha1.KindKCT, oldKCT)
		}

		filtered := make([]v1alpha1.ResourceRef, 0, len(claim.Status.PendingDeletion))
		for _, ref := range claim.Status.PendingDeletion {
			if (ref.Kind == v1alpha1.KindBMT && ref.Name == bmtName) ||
				(ref.Kind == v1alpha1.KindKCT && ref.Name == kctName) {
				continue
			}
			filtered = append(filtered, ref)
		}
		claim.Status.PendingDeletion = filtered
	}

	mdUpdated, err := r.ensureMachineDeployment(ctx, claim, nodeLabels, bmtName, kctName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure MD: %w", err)
	}
	if mdUpdated {
		r.Recorder.Eventf(claim, "Normal", "MachineDeploymentUpdated",
			"Updated MachineDeployment %s",
			builder.MachineDeploymentName(claim.Spec.ClusterName, claim.Name))
	}

	if err := r.ensureMachineHealthCheck(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure MHC: %w", err)
	}

	claim.Status.CurrentTemplates = &v1alpha1.CurrentTemplates{BMT: bmtName, KCT: kctName}
	claim.Status.LastRendered = &v1alpha1.LastRendered{BMTHash: bmtHash, KCTHash: kctHash}
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionTemplatesRendered,
		Status:             metav1.ConditionTrue,
		Reason:             "RenderSuccess",
		Message:            "All templates rendered successfully",
		ObservedGeneration: claim.Generation,
	})

	r.mirrorMDStatus(ctx, claim)

	claim.Status.ObservedGeneration = claim.Generation

	if hashChanged {
		return r.startRollout(ctx, claim)
	}

	if len(claim.Status.PendingDeletion) > 0 {
		return r.trackRollout(ctx, claim)
	}

	return r.setReady(ctx, claim)
}

// startRollout transitions to Updating phase and records rollout start.
func (r *WorkerGroupClaimReconciler) startRollout(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	claim.Status.Phase = v1alpha1.PhaseUpdating
	claim.Status.RolloutStartedAt = &metav1.Time{Time: time.Now()}

	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionRolloutComplete,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutStarted",
		Message:            "Rolling out new templates",
		ObservedGeneration: claim.Generation,
	})
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionRolloutTimedOut,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutStarted",
		Message:            "New rollout started",
		ObservedGeneration: claim.Generation,
	})
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutInProgress",
		Message:            "Rollout in progress",
		ObservedGeneration: claim.Generation,
	})

	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status for rollout start: %w", err)
	}

	r.Recorder.Event(claim, "Normal", "RolloutStarted", "Started rolling out new templates")
	log.Info("Rollout started", "pendingDeletion", len(claim.Status.PendingDeletion))

	return ctrl.Result{RequeueAfter: rolloutRequeueDelay}, nil
}

// reconcileDelete handles ordered cleanup: MD → BMT → KCT → pendingDeletion → remove finalizer.
func (r *WorkerGroupClaimReconciler) reconcileDelete(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(claim, v1alpha1.WorkerGroupClaimFinalizer) {
		return ctrl.Result{}, nil
	}

	// Set Deleting phase
	if claim.Status.Phase != v1alpha1.PhaseDeleting {
		claim.Status.Phase = v1alpha1.PhaseDeleting
		if err := r.Status().Update(ctx, claim); err != nil {
			return ctrl.Result{}, fmt.Errorf("set deleting phase: %w", err)
		}
	}

	mhcName := builder.MachineHealthCheckName(claim.Spec.ClusterName, claim.Name)
	mhc := &clusterv1.MachineHealthCheck{}
	switch err := r.Get(ctx, types.NamespacedName{Name: mhcName, Namespace: claim.Namespace}, mhc); {
	case err == nil:
		if delErr := r.Delete(ctx, mhc); delErr != nil && !apierrors.IsNotFound(delErr) {
			return ctrl.Result{}, fmt.Errorf("delete MHC %q: %w", mhcName, delErr)
		}
		log.Info("Deleted MachineHealthCheck", "mhc", mhcName)
	case !apierrors.IsNotFound(err):
		return ctrl.Result{}, fmt.Errorf("get MHC %q: %w", mhcName, err)
	}

	// Step 1: Delete MachineDeployment first (it references BMT/KCT)
	mdName := builder.MachineDeploymentName(claim.Spec.ClusterName, claim.Name)
	md := &clusterv1.MachineDeployment{}
	err := r.Get(ctx, types.NamespacedName{Name: mdName, Namespace: claim.Namespace}, md)
	if err == nil {
		if err := r.Delete(ctx, md); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete MD %q: %w", mdName, err)
		}
		r.Recorder.Eventf(claim, "Normal", "MachineDeploymentDeleted",
			"Deleted MachineDeployment %s", mdName)
		log.Info("Deleted MachineDeployment, waiting for cleanup", "md", mdName)

		return ctrl.Result{RequeueAfter: deleteRequeueDelay}, nil
	}
	if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("get MD %q: %w", mdName, err)
	}

	// Step 2: Delete current templates
	if claim.Status.CurrentTemplates != nil {
		if claim.Status.CurrentTemplates.BMT != "" {
			if delErr := r.deleteOldTemplate(ctx, claim.Namespace, v1alpha1.ResourceRef{
				Kind: v1alpha1.KindBMT, Name: claim.Status.CurrentTemplates.BMT,
			}); delErr != nil {
				log.Error(delErr, "Failed to delete current BMT", "name", claim.Status.CurrentTemplates.BMT)
			}
		}
		if claim.Status.CurrentTemplates.KCT != "" {
			if delErr := r.deleteOldTemplate(ctx, claim.Namespace, v1alpha1.ResourceRef{
				Kind: v1alpha1.KindKCT, Name: claim.Status.CurrentTemplates.KCT,
			}); delErr != nil {
				log.Error(delErr, "Failed to delete current KCT", "name", claim.Status.CurrentTemplates.KCT)
			}
		}
	}

	// Step 3: Delete pendingDeletion templates
	for _, ref := range claim.Status.PendingDeletion {
		if delErr := r.deleteOldTemplate(ctx, claim.Namespace, ref); delErr != nil {
			log.Error(delErr, "Failed to delete pending template", "kind", ref.Kind, "name", ref.Name)
		}
	}

	// Step 4: Remove finalizer
	controllerutil.RemoveFinalizer(claim, v1alpha1.WorkerGroupClaimFinalizer)
	if err := r.Update(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}

	r.Recorder.Event(claim, "Normal", "Deleted", "WorkerGroupClaim deleted successfully")
	log.Info("Deletion complete, finalizer removed")

	return ctrl.Result{}, nil
}

// reconcilePaused handles the paused state: sets phase Paused, condition, no requeue.
func (r *WorkerGroupClaimReconciler) reconcilePaused(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
) (ctrl.Result, error) {
	if claim.Status.Phase == v1alpha1.PhasePaused {
		return ctrl.Result{}, nil // already paused, no requeue
	}

	claim.Status.Phase = v1alpha1.PhasePaused
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionPaused,
		Status:             metav1.ConditionTrue,
		Reason:             "Paused",
		Message:            "Reconciliation paused via annotation",
		ObservedGeneration: claim.Generation,
	})
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Paused",
		Message:            "Reconciliation paused",
		ObservedGeneration: claim.Generation,
	})

	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("set paused status: %w", err)
	}

	r.Recorder.Event(claim, "Normal", "Paused", "Reconciliation paused")

	return ctrl.Result{}, nil
}

// trackRollout checks rollout progress, handles timeout, and completes cleanup.
func (r *WorkerGroupClaimReconciler) trackRollout(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	mdName := builder.MachineDeploymentName(claim.Spec.ClusterName, claim.Name)
	md := &clusterv1.MachineDeployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: mdName, Namespace: claim.Namespace}, md); err != nil {
		return ctrl.Result{}, fmt.Errorf("get MD for rollout tracking: %w", err)
	}

	// Check if rollout is complete → cleanup and Ready
	if isRolloutComplete(md) {
		return r.completeRollout(ctx, claim)
	}

	// Check for timeout → Degraded
	if claim.Status.RolloutStartedAt != nil {
		timeout := defaultRolloutTimeout
		if claim.Spec.RolloutTimeout != nil {
			timeout = claim.Spec.RolloutTimeout.Duration
		}
		if time.Since(claim.Status.RolloutStartedAt.Time) > timeout {
			if claim.Status.Phase != v1alpha1.PhaseDegraded {
				claim.Status.Phase = v1alpha1.PhaseDegraded
				meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
					Type:               v1alpha1.ConditionRolloutTimedOut,
					Status:             metav1.ConditionTrue,
					Reason:             "RolloutTimeout",
					Message:            fmt.Sprintf("Rollout timed out after %s", timeout),
					ObservedGeneration: claim.Generation,
				})
				meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
					Type:               v1alpha1.ConditionReady,
					Status:             metav1.ConditionFalse,
					Reason:             "RolloutTimeout",
					Message:            "Rollout timed out",
					ObservedGeneration: claim.Generation,
				})
				r.Recorder.Eventf(claim, "Warning", "RolloutTimeout",
					"Rollout timed out after %s (no auto-rollback)", timeout)
				log.Info("Rollout timed out", "timeout", timeout)
			}

			if err := r.Status().Update(ctx, claim); err != nil {
				return ctrl.Result{}, fmt.Errorf("update status for degraded: %w", err)
			}

			return ctrl.Result{RequeueAfter: degradedRequeueDelay}, nil
		}
	}

	// Still rolling out → requeue
	claim.Status.Phase = v1alpha1.PhaseUpdating
	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status for rollout tracking: %w", err)
	}

	log.V(1).Info("Rollout in progress, requeuing", "pendingDeletion", len(claim.Status.PendingDeletion))

	return ctrl.Result{RequeueAfter: rolloutRequeueDelay}, nil
}

// completeRollout deletes old templates from pendingDeletion and transitions to Ready.
// Before deleting, checks that no MachineSet still references the template.
func (r *WorkerGroupClaimReconciler) completeRollout(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	mdName := builder.MachineDeploymentName(claim.Spec.ClusterName, claim.Name)

	var stillReferenced []v1alpha1.ResourceRef
	for _, ref := range claim.Status.PendingDeletion {
		inUse, err := r.isTemplateReferencedByMachineSet(ctx, claim.Namespace, mdName, ref)
		if err != nil {
			log.Error(err, "Failed to check MachineSet references", "kind", ref.Kind, "name", ref.Name)
			stillReferenced = append(stillReferenced, ref)

			continue
		}
		if inUse {
			log.Info("Template still referenced by MachineSet, deferring deletion",
				"kind", ref.Kind, "name", ref.Name)
			stillReferenced = append(stillReferenced, ref)

			continue
		}
		if err := r.deleteOldTemplate(ctx, claim.Namespace, ref); err != nil {
			log.Error(err, "Failed to delete old template", "kind", ref.Kind, "name", ref.Name)
			stillReferenced = append(stillReferenced, ref)
		} else {
			r.Recorder.Eventf(claim, "Normal", "StaleTemplateDeleted",
				"Deleted old %s %s", ref.Kind, ref.Name)
		}
	}

	// If some templates are still in use by a MachineSet, keep them in pendingDeletion and requeue.
	if len(stillReferenced) > 0 {
		claim.Status.PendingDeletion = stillReferenced
		if err := r.Status().Update(ctx, claim); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status for partial cleanup: %w", err)
		}
		log.Info("Some templates still referenced by MachineSets, requeuing",
			"remaining", len(stillReferenced))

		return ctrl.Result{RequeueAfter: rolloutRequeueDelay}, nil
	}

	claim.Status.PendingDeletion = nil
	claim.Status.RolloutStartedAt = nil
	claim.Status.Phase = v1alpha1.PhaseReady

	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionRolloutComplete,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutComplete",
		Message:            "Rollout completed, old templates deleted",
		ObservedGeneration: claim.Generation,
	})
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionRolloutTimedOut,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutComplete",
		Message:            "Rollout completed",
		ObservedGeneration: claim.Generation,
	})
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "All resources provisioned, rollout complete",
		ObservedGeneration: claim.Generation,
	})

	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status for rollout complete: %w", err)
	}

	r.Recorder.Event(claim, "Normal", "RolloutComplete", "Rollout completed successfully")
	log.Info("Rollout completed")

	return ctrl.Result{}, nil
}

// deleteOldTemplate deletes a BMT or KCT by reference.
func (r *WorkerGroupClaimReconciler) deleteOldTemplate(
	ctx context.Context, namespace string, ref v1alpha1.ResourceRef,
) error {
	switch ref.Kind {
	case v1alpha1.KindBMT:
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   v1alpha1.BMTGroup,
			Version: v1alpha1.BMTVersion,
			Kind:    v1alpha1.KindBMT,
		})
		obj.SetName(ref.Name)
		obj.SetNamespace(namespace)
		if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete BMT %q: %w", ref.Name, err)
		}
	case v1alpha1.KindKCT:
		obj := &bootstrapv1.KubeadmConfigTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: ref.Name, Namespace: namespace},
		}
		if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete KCT %q: %w", ref.Name, err)
		}
	default:
		return fmt.Errorf("unknown resource kind: %s", ref.Kind)
	}

	return nil
}

// isTemplateReferencedByMachineSet checks if any MachineSet belonging to the given
// MachineDeployment still references the template (KCT via bootstrap.configRef,
// BMT via infrastructureRef). MachineSets are found by the standard CAPI label
// "cluster.x-k8s.io/deployment-name".
func (r *WorkerGroupClaimReconciler) isTemplateReferencedByMachineSet(
	ctx context.Context, namespace, mdName string, ref v1alpha1.ResourceRef,
) (bool, error) {
	msList := &clusterv1.MachineSetList{}
	if err := r.List(ctx, msList,
		client.InNamespace(namespace),
		client.MatchingLabels{"cluster.x-k8s.io/deployment-name": mdName},
	); err != nil {
		return false, fmt.Errorf("list MachineSets for MD %q: %w", mdName, err)
	}

	for i := range msList.Items {
		ms := &msList.Items[i]
		switch ref.Kind {
		case v1alpha1.KindKCT:
			if ms.Spec.Template.Spec.Bootstrap.ConfigRef.Name == ref.Name {
				return true, nil
			}
		case v1alpha1.KindBMT:
			if ms.Spec.Template.Spec.InfrastructureRef.Name == ref.Name {
				return true, nil
			}
		}
	}

	return false, nil
}

// mirrorMDStatus populates claim.status.machineDeployment from the real MD.
func (r *WorkerGroupClaimReconciler) mirrorMDStatus(ctx context.Context, claim *v1alpha1.WorkerGroupClaim) {
	mdName := builder.MachineDeploymentName(claim.Spec.ClusterName, claim.Name)
	md := &clusterv1.MachineDeployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: mdName, Namespace: claim.Namespace}, md); err != nil {
		return // MD might not exist yet
	}

	mdStatus := &v1alpha1.MachineDeploymentStatus{}
	if md.Status.Replicas != nil {
		mdStatus.Replicas = *md.Status.Replicas
	}
	if md.Status.ReadyReplicas != nil {
		mdStatus.ReadyReplicas = *md.Status.ReadyReplicas
	}
	if md.Status.UpToDateReplicas != nil {
		mdStatus.UpToDateReplicas = *md.Status.UpToDateReplicas
	}
	claim.Status.MachineDeployment = mdStatus
}

// setReady transitions to Ready phase.
func (r *WorkerGroupClaimReconciler) setReady(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	claim.Status.Phase = v1alpha1.PhaseReady
	claim.Status.RolloutStartedAt = nil

	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "All resources provisioned",
		ObservedGeneration: claim.Generation,
	})

	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	log.Info("Reconcile complete", "phase", claim.Status.Phase,
		"bmt", claim.Status.CurrentTemplates.BMT, "kct", claim.Status.CurrentTemplates.KCT)

	return ctrl.Result{}, nil
}

// ensureFinalizer adds the finalizer if not present.
func (r *WorkerGroupClaimReconciler) ensureFinalizer(ctx context.Context, claim *v1alpha1.WorkerGroupClaim) error {
	if controllerutil.ContainsFinalizer(claim, v1alpha1.WorkerGroupClaimFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(claim, v1alpha1.WorkerGroupClaimFinalizer)

	// Set initial phase
	if claim.Status.Phase == "" {
		claim.Status.Phase = v1alpha1.PhaseProvisioning
		if err := r.Status().Update(ctx, claim); err != nil {
			return fmt.Errorf("set initial phase: %w", err)
		}
	}

	return r.Update(ctx, claim)
}

// isPaused returns true if the claim has the pause annotation.
func isPaused(claim *v1alpha1.WorkerGroupClaim) bool {
	return claim.Annotations[v1alpha1.PausedAnnotation] == "true"
}

// renderTemplates loads templates, prepares vars, injects auto-vars, and renders.
func (r *WorkerGroupClaimReconciler) renderTemplates(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim, nodeLabels map[string]string,
) (bmtYAML, kctYAML string, err error) {
	// Load WGMachineTemplate
	machineTemplate := &v1alpha1.WGMachineTemplate{}
	if err := r.Get(ctx, types.NamespacedName{Name: claim.Spec.MachineTemplateRef.Name}, machineTemplate); err != nil {
		if apierrors.IsNotFound(err) {
			r.Recorder.Eventf(claim, "Warning", "TemplateNotFound",
				"WGMachineTemplate %q not found", claim.Spec.MachineTemplateRef.Name)
		}

		return "", "", fmt.Errorf("get WGMachineTemplate %q: %w", claim.Spec.MachineTemplateRef.Name, err)
	}

	// Load WGBootstrapTemplate
	bootstrapTemplate := &v1alpha1.WGBootstrapTemplate{}
	if err := r.Get(ctx, types.NamespacedName{Name: claim.Spec.BootstrapTemplateRef.Name}, bootstrapTemplate); err != nil {
		if apierrors.IsNotFound(err) {
			r.Recorder.Eventf(claim, "Warning", "TemplateNotFound",
				"WGBootstrapTemplate %q not found", claim.Spec.BootstrapTemplateRef.Name)
		}

		return "", "", fmt.Errorf("get WGBootstrapTemplate %q: %w", claim.Spec.BootstrapTemplateRef.Name, err)
	}

	// Render BMT
	infraVars, err := renderer.PrepareVars(claim.Spec.Infrastructure)
	if err != nil {
		return "", "", fmt.Errorf("prepare infrastructure vars: %w", err)
	}
	bmtYAML, err = renderer.Render(machineTemplate.Spec.Value, infraVars)
	if err != nil {
		r.Recorder.Eventf(claim, "Warning", "RenderError", "BMT render failed: %v", err)

		return "", "", fmt.Errorf("render BMT: %w", err)
	}

	// Render KCT
	bootstrapVars, err := renderer.PrepareVars(claim.Spec.Bootstrap)
	if err != nil {
		return "", "", fmt.Errorf("prepare bootstrap vars: %w", err)
	}

	// Auto-inject nodeLabels and machineDeploymentName
	renderer.InjectNodeLabels(bootstrapVars, nodeLabels)
	renderer.InjectMachineDeploymentName(bootstrapVars, claim.Spec.ClusterName, claim.Name)

	// Inject kubeletConfigYaml
	kubeletConfigYAML, err := r.renderKubeletConfig(claim)
	if err != nil {
		return "", "", fmt.Errorf("render kubelet config: %w", err)
	}
	bootstrapVars["__kubeletConfigYaml"] = kubeletConfigYAML

	kctYAML, err = renderer.Render(bootstrapTemplate.Spec.Value, bootstrapVars)
	if err != nil {
		r.Recorder.Eventf(claim, "Warning", "RenderError", "KCT render failed: %v", err)

		return "", "", fmt.Errorf("render KCT: %w", err)
	}

	r.Recorder.Event(claim, "Normal", "TemplateRendered", "Templates rendered successfully")

	return bmtYAML, kctYAML, nil
}

// renderKubeletConfig merges kubelet configuration overrides with defaults.
func (r *WorkerGroupClaimReconciler) renderKubeletConfig(claim *v1alpha1.WorkerGroupClaim) (string, error) {
	if claim.Spec.KubeletConfiguration == nil {
		return kubelet.MergeKubeletConfig(nil)
	}

	// Convert typed struct to map via JSON roundtrip
	data, err := json.Marshal(claim.Spec.KubeletConfiguration)
	if err != nil {
		return "", fmt.Errorf("marshal kubelet overrides: %w", err)
	}
	overrides, err := kubelet.OverridesToMap(data)
	if err != nil {
		return "", err
	}

	return kubelet.MergeKubeletConfig(overrides)
}

// ensureBMT creates a BegetMachineTemplate if it doesn't exist. Returns true if created.
func (r *WorkerGroupClaimReconciler) ensureBMT(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
	renderedYAML, name string,
) (bool, error) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   v1alpha1.BMTGroup,
		Version: v1alpha1.BMTVersion,
		Kind:    v1alpha1.KindBMT,
	})
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: claim.Namespace}, existing)
	if err == nil {
		return false, nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("get BMT %q: %w", name, err)
	}

	bmt, err := builder.BuildBMT(renderedYAML, name, claim.Namespace, claim)
	if err != nil {
		return false, fmt.Errorf("build BMT: %w", err)
	}

	if err := r.Create(ctx, bmt); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}

		return false, fmt.Errorf("create BMT %q: %w", name, err)
	}

	return true, nil
}

// ensureKCT creates or in-place updates a KubeadmConfigTemplate.
// Returns (created, updated, error). In-place update handles kubelet config changes
// where the hash-based name stays the same but content differs.
func (r *WorkerGroupClaimReconciler) ensureKCT(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
	renderedYAML, name string,
) (bool, bool, error) {
	existing := &bootstrapv1.KubeadmConfigTemplate{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: claim.Namespace}, existing)
	if err == nil {
		// KCT exists — check for in-place update (e.g., kubelet config change)
		desired, buildErr := builder.BuildKCT(renderedYAML, name, claim.Namespace, claim)
		if buildErr != nil {
			return false, false, fmt.Errorf("build KCT for comparison: %w", buildErr)
		}
		if !apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
			existing.Spec = desired.Spec
			if updateErr := r.Update(ctx, existing); updateErr != nil {
				return false, false, fmt.Errorf("update KCT %q in-place: %w", name, updateErr)
			}

			return false, true, nil
		}

		return false, false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, false, fmt.Errorf("get KCT %q: %w", name, err)
	}

	kct, err := builder.BuildKCT(renderedYAML, name, claim.Namespace, claim)
	if err != nil {
		return false, false, fmt.Errorf("build KCT: %w", err)
	}

	if err := r.Create(ctx, kct); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, false, nil
		}

		return false, false, fmt.Errorf("create KCT %q: %w", name, err)
	}

	return true, false, nil
}

// ensureMachineDeployment creates or updates the MachineDeployment. Returns true if updated.
func (r *WorkerGroupClaimReconciler) ensureMachineDeployment(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
	nodeLabels map[string]string,
	bmtName, kctName string,
) (bool, error) {
	mdName := builder.MachineDeploymentName(claim.Spec.ClusterName, claim.Name)

	desired := builder.BuildMachineDeployment(claim, nodeLabels, bmtName, kctName)

	existing := &clusterv1.MachineDeployment{}
	err := r.Get(ctx, types.NamespacedName{Name: mdName, Namespace: claim.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return false, fmt.Errorf("create MD %q: %w", mdName, err)
		}

		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get MD %q: %w", mdName, err)
	}

	diff := builder.ComputeMDDiff(desired, existing)
	if !diff.NeedsUpdate() {
		return false, nil
	}

	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Template.Spec.Version = desired.Spec.Template.Spec.Version
	existing.Spec.Template.Spec.InfrastructureRef = desired.Spec.Template.Spec.InfrastructureRef
	existing.Spec.Template.Spec.Bootstrap = desired.Spec.Template.Spec.Bootstrap
	existing.Spec.Template.Labels = desired.Spec.Template.Labels
	existing.Spec.Template.Spec.Taints = desired.Spec.Template.Spec.Taints
	existing.Spec.Rollout = desired.Spec.Rollout
	existing.Spec.Deletion = desired.Spec.Deletion
	existing.Spec.Template.Spec.Deletion = desired.Spec.Template.Spec.Deletion
	existing.Spec.Remediation = desired.Spec.Remediation

	if err := r.Update(ctx, existing); err != nil {
		return false, fmt.Errorf("update MD %q: %w", mdName, err)
	}

	return true, nil
}

func (r *WorkerGroupClaimReconciler) ensureMachineHealthCheck(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
) error {
	log := logf.FromContext(ctx)
	name := builder.MachineHealthCheckName(claim.Spec.ClusterName, claim.Name)
	key := types.NamespacedName{Name: name, Namespace: claim.Namespace}

	desired := builder.BuildMachineHealthCheck(claim)
	existing := &clusterv1.MachineHealthCheck{}
	err := r.Get(ctx, key, existing)

	switch {
	case desired == nil && apierrors.IsNotFound(err):
		claim.Status.HealthCheck = nil
		r.setAutohealingCondition(claim, false, "Disabled", "Autohealing is disabled for this worker group")
		return nil

	case desired == nil:
		if err != nil {
			return fmt.Errorf("get MHC %q: %w", name, err)
		}
		if delErr := r.Delete(ctx, existing); delErr != nil && !apierrors.IsNotFound(delErr) {
			return fmt.Errorf("delete MHC %q: %w", name, delErr)
		}
		log.Info("Deleted MachineHealthCheck", "mhc", name)
		r.Recorder.Eventf(claim, "Normal", "AutohealingDisabled", "Deleted MachineHealthCheck %s", name)
		claim.Status.HealthCheck = nil
		r.setAutohealingCondition(claim, false, "Disabled", "Autohealing is disabled for this worker group")
		return nil

	case apierrors.IsNotFound(err):
		if createErr := r.Create(ctx, desired); createErr != nil {
			r.setAutohealingCondition(claim, false, "CreateFailed", createErr.Error())
			return fmt.Errorf("create MHC %q: %w", name, createErr)
		}
		log.Info("Created MachineHealthCheck", "mhc", name)
		r.Recorder.Eventf(claim, "Normal", "AutohealingEnabled", "Created MachineHealthCheck %s", name)
		r.mirrorMHCStatus(claim, desired)
		return nil

	case err != nil:
		return fmt.Errorf("get MHC %q: %w", name, err)
	}

	if !apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		if updErr := r.Update(ctx, existing); updErr != nil {
			r.setAutohealingCondition(claim, false, "UpdateFailed", updErr.Error())
			return fmt.Errorf("update MHC %q: %w", name, updErr)
		}
		log.Info("Updated MachineHealthCheck", "mhc", name)
		r.Recorder.Eventf(claim, "Normal", "AutohealingUpdated", "Updated MachineHealthCheck %s", name)
	}
	r.mirrorMHCStatus(claim, existing)
	return nil
}

func (r *WorkerGroupClaimReconciler) mirrorMHCStatus(
	claim *v1alpha1.WorkerGroupClaim, mhc *clusterv1.MachineHealthCheck,
) {
	status := &v1alpha1.HealthCheckStatus{Enabled: true, Name: mhc.Name}
	if c := meta.FindStatusCondition(mhc.Status.Conditions, clusterv1.MachineHealthCheckRemediationAllowedCondition); c != nil {
		allowed := c.Status == metav1.ConditionTrue
		status.RemediationAllowed = &allowed
	}
	claim.Status.HealthCheck = status
	r.setAutohealingCondition(claim, true, "MachineHealthCheckReady",
		fmt.Sprintf("MachineHealthCheck %s is in place", mhc.Name))
}

func (r *WorkerGroupClaimReconciler) setAutohealingCondition(
	claim *v1alpha1.WorkerGroupClaim, ok bool, reason, message string,
) {
	status := metav1.ConditionFalse
	if ok {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionAutohealingConfigured,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: claim.Generation,
	})
}

// labelPolicy resolves the deny policy from the CRD, falling back to the compiled-in floor.
func (r *WorkerGroupClaimReconciler) labelPolicy(ctx context.Context) *nodelabels.Policy {
	obj := &v1alpha1.NodeLabelPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: v1alpha1.NodeLabelPolicyName}, obj)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			logf.FromContext(ctx).Error(err, "Failed to read NodeLabelPolicy, using compiled-in rules")
		}

		return nodelabels.NewClaimPolicy(nil)
	}

	return nodelabels.NewClaimPolicy(obj.Spec.DenyPatterns)
}

// setNodeLabelsAcceptedCondition reports which nodeLabels were dropped by policy.
func (r *WorkerGroupClaimReconciler) setNodeLabelsAcceptedCondition(
	claim *v1alpha1.WorkerGroupClaim, rejected []string,
) {
	cond := metav1.Condition{
		Type:               v1alpha1.ConditionNodeLabelsAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "AllLabelsAccepted",
		Message:            "All nodeLabels accepted",
		ObservedGeneration: claim.Generation,
	}

	if len(rejected) != 0 {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "ReservedPrefixesDropped"
		cond.Message = fmt.Sprintf("Dropped nodeLabels with reserved prefixes: %s", strings.Join(rejected, ", "))
		r.Recorder.Event(claim, "Warning", cond.Reason, cond.Message)
	}

	meta.SetStatusCondition(&claim.Status.Conditions, cond)
}

// setFailed sets the claim to Failed phase with condition and returns a delayed requeue.
func (r *WorkerGroupClaimReconciler) setFailed(
	ctx context.Context, claim *v1alpha1.WorkerGroupClaim,
	reason, message string,
) (ctrl.Result, error) {
	claim.Status.Phase = v1alpha1.PhaseFailed
	claim.Status.ObservedGeneration = claim.Generation
	if claim.Status.LastRendered == nil {
		claim.Status.LastRendered = &v1alpha1.LastRendered{}
	}
	claim.Status.LastRendered.Error = message

	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionTemplatesRendered,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: claim.Generation,
	})
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: claim.Generation,
	})

	if err := r.Status().Update(ctx, claim); err != nil {
		return ctrl.Result{}, fmt.Errorf("update failed status: %w", err)
	}

	r.Recorder.Eventf(claim, "Warning", reason, "%s", message)

	return ctrl.Result{RequeueAfter: failedRequeueDelay}, nil
}

// appendResourceRef adds a resource ref to the list if not already present.
func appendResourceRef(refs []v1alpha1.ResourceRef, kind, name string) []v1alpha1.ResourceRef {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			return refs // already present
		}
	}

	return append(refs, v1alpha1.ResourceRef{Kind: kind, Name: name})
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkerGroupClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.WorkerGroupClaim{}).
		// Owned resources — automatic enqueue of owner
		Owns(&clusterv1.MachineDeployment{}).
		Owns(&clusterv1.MachineHealthCheck{}, builderpkg.WithPredicates(predicate.GenerationChangedPredicate{})).
		// Shared templates — enqueue all referencing Claims
		Watches(&v1alpha1.WGBootstrapTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.findClaimsForBootstrapTemplate)).
		Watches(&v1alpha1.WGMachineTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.findClaimsForMachineTemplate)).
		Watches(&v1alpha1.NodeLabelPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.findAllClaims)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
		}).
		Named("workergroupclaim").
		Complete(r)
}

// findClaimsForBootstrapTemplate returns reconcile requests for all Claims
// referencing the changed WGBootstrapTemplate.
func (r *WorkerGroupClaimReconciler) findClaimsForBootstrapTemplate(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	return r.findClaimsReferencingTemplate(ctx, obj.GetName(), "bootstrapTemplateRef")
}

// findClaimsForMachineTemplate returns reconcile requests for all Claims
// referencing the changed WGMachineTemplate.
func (r *WorkerGroupClaimReconciler) findClaimsForMachineTemplate(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	return r.findClaimsReferencingTemplate(ctx, obj.GetName(), "machineTemplateRef")
}

// findAllClaims re-queues every claim on a policy change.
func (r *WorkerGroupClaimReconciler) findAllClaims(
	ctx context.Context, _ client.Object,
) []reconcile.Request {
	claims := &v1alpha1.WorkerGroupClaimList{}
	if err := r.List(ctx, claims); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list claims for policy change")

		return nil
	}

	requests := make([]reconcile.Request, 0, len(claims.Items))
	for i := range claims.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      claims.Items[i].Name,
				Namespace: claims.Items[i].Namespace,
			},
		})
	}

	return requests
}

// findClaimsReferencingTemplate lists all Claims and filters by template ref name.
func (r *WorkerGroupClaimReconciler) findClaimsReferencingTemplate(
	ctx context.Context, templateName, refField string,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	claims := &v1alpha1.WorkerGroupClaimList{}
	if err := r.List(ctx, claims); err != nil {
		log.Error(err, "failed to list WorkerGroupClaims for template watch")

		return nil
	}

	var requests []reconcile.Request
	for i := range claims.Items {
		claim := &claims.Items[i]
		var match bool
		switch refField {
		case "bootstrapTemplateRef":
			match = claim.Spec.BootstrapTemplateRef.Name == templateName
		case "machineTemplateRef":
			match = claim.Spec.MachineTemplateRef.Name == templateName
		}
		if match {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      claim.Name,
					Namespace: claim.Namespace,
				},
			})
		}
	}

	if len(requests) > 0 {
		log.Info("Template changed, enqueuing referencing claims",
			"template", templateName, "refField", refField, "count", len(requests))
	}

	return requests
}
