package controller

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
)

const (
	defaultOrphanInterval    = 5 * time.Minute
	defaultOrphanGracePeriod = 10 * time.Minute
)

// OrphanCleaner periodically deletes BMT/KCT resources that are not referenced
// by any WorkerGroupClaim and are older than the grace period.
type OrphanCleaner struct {
	client.Client
	Recorder    record.EventRecorder
	Interval    time.Duration
	GracePeriod time.Duration
}

// Start implements manager.Runnable. It runs cleanup on a periodic interval.
func (c *OrphanCleaner) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("orphan-cleaner")
	log.Info("Starting orphan cleaner", "interval", c.Interval, "gracePeriod", c.GracePeriod)

	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping orphan cleaner")

			return nil
		case <-ticker.C:
			c.CleanupOnce(ctx)
		}
	}
}

// NeedLeaderElection returns true — orphan cleanup should only run on the leader.
func (c *OrphanCleaner) NeedLeaderElection() bool {
	return true
}

// CleanupOnce runs a single orphan cleanup pass. Exported for testing.
func (c *OrphanCleaner) CleanupOnce(ctx context.Context) {
	log := logf.FromContext(ctx).WithName("orphan-cleaner")

	// List all labeled BMTs
	bmtList := &unstructured.UnstructuredList{}
	bmtList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   v1alpha1.BMTGroup,
		Version: v1alpha1.BMTVersion,
		Kind:    "BegetMachineTemplateList",
	})
	if err := c.List(ctx, bmtList, client.HasLabels{v1alpha1.LabelClaimName}); err != nil {
		log.Error(err, "Failed to list labeled BMTs")

		return
	}

	// List all labeled KCTs
	kctList := &bootstrapv1.KubeadmConfigTemplateList{}
	if err := c.List(ctx, kctList, client.HasLabels{v1alpha1.LabelClaimName}); err != nil {
		log.Error(err, "Failed to list labeled KCTs")

		return
	}

	// List all WorkerGroupClaims
	claimList := &v1alpha1.WorkerGroupClaimList{}
	if err := c.List(ctx, claimList); err != nil {
		log.Error(err, "Failed to list WorkerGroupClaims")

		return
	}

	// Build referenced sets
	referencedBMTs, referencedKCTs := findReferencedTemplates(claimList.Items)
	now := time.Now()

	// Clean orphan BMTs
	for i := range bmtList.Items {
		bmt := &bmtList.Items[i]
		if !isOrphanTemplate(bmt.GetName(), bmt.GetCreationTimestamp().Time, referencedBMTs, c.GracePeriod, now) {
			continue
		}
		log.Info("Deleting orphan BMT", "name", bmt.GetName(), "namespace", bmt.GetNamespace(),
			"age", now.Sub(bmt.GetCreationTimestamp().Time).Round(time.Second))
		if c.Recorder != nil {
			c.Recorder.Eventf(bmt, "Normal", "OrphanTemplateDeleted",
				"Deleted orphan BegetMachineTemplate %s", bmt.GetName())
		}
		if err := c.Delete(ctx, bmt); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to delete orphan BMT", "name", bmt.GetName())
		}
	}

	// Clean orphan KCTs
	for i := range kctList.Items {
		kct := &kctList.Items[i]
		if !isOrphanTemplate(kct.Name, kct.CreationTimestamp.Time, referencedKCTs, c.GracePeriod, now) {
			continue
		}
		log.Info("Deleting orphan KCT", "name", kct.Name, "namespace", kct.Namespace,
			"age", now.Sub(kct.CreationTimestamp.Time).Round(time.Second))
		if c.Recorder != nil {
			c.Recorder.Eventf(kct, "Normal", "OrphanTemplateDeleted",
				"Deleted orphan KubeadmConfigTemplate %s", kct.Name)
		}
		if err := c.Delete(ctx, kct); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to delete orphan KCT", "name", kct.Name)
		}
	}
}

// isOrphanTemplate checks if a template is an orphan: not referenced and older than grace period.
func isOrphanTemplate(name string, createdAt time.Time, referenced map[string]bool, gracePeriod time.Duration, now time.Time) bool {
	if referenced[name] {
		return false
	}

	return now.Sub(createdAt) >= gracePeriod
}

// findReferencedTemplates builds sets of all template names referenced by any claim
// (both currentTemplates and pendingDeletion).
func findReferencedTemplates(claims []v1alpha1.WorkerGroupClaim) (bmtNames, kctNames map[string]bool) {
	bmtNames = make(map[string]bool)
	kctNames = make(map[string]bool)

	for i := range claims {
		claim := &claims[i]
		if claim.Status.CurrentTemplates != nil {
			if claim.Status.CurrentTemplates.BMT != "" {
				bmtNames[claim.Status.CurrentTemplates.BMT] = true
			}
			if claim.Status.CurrentTemplates.KCT != "" {
				kctNames[claim.Status.CurrentTemplates.KCT] = true
			}
		}
		for _, ref := range claim.Status.PendingDeletion {
			switch ref.Kind {
			case v1alpha1.KindBMT:
				bmtNames[ref.Name] = true
			case v1alpha1.KindKCT:
				kctNames[ref.Name] = true
			}
		}
	}

	return bmtNames, kctNames
}

// NewOrphanCleaner creates an OrphanCleaner with the given client and recorder.
func NewOrphanCleaner(c client.Client, recorder record.EventRecorder) *OrphanCleaner {
	return &OrphanCleaner{
		Client:      c,
		Recorder:    recorder,
		Interval:    defaultOrphanInterval,
		GracePeriod: defaultOrphanGracePeriod,
	}
}

// ownerRef returns a minimal OwnerReference for use in orphan detection.
// Needed to avoid circular import with builder package.
func orphanBMTGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   v1alpha1.BMTGroup,
		Version: v1alpha1.BMTVersion,
		Kind:    v1alpha1.KindBMT,
	}
}

// Helper to create a labeled BMT for testing.
func newLabeledBMT(name, namespace, claimName, clusterName string, createdAt metav1.Time) *unstructured.Unstructured {
	bmt := &unstructured.Unstructured{}
	bmt.SetGroupVersionKind(orphanBMTGVK())
	bmt.SetName(name)
	bmt.SetNamespace(namespace)
	bmt.SetLabels(map[string]string{
		v1alpha1.LabelClaimName:   claimName,
		v1alpha1.LabelClusterName: clusterName,
	})
	bmt.SetCreationTimestamp(createdAt)

	return bmt
}
