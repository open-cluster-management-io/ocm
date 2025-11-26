package managedcluster

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	commonrecorder "open-cluster-management.io/ocm/pkg/common/recorder"
)

const (
	// ClusterSetLabelPrefix for hub-specific clusterset labels using hub hash
	ClusterSetLabelPrefix = "clusterset.open-cluster-management.io/"

	// MaxLabelNameLength is the maximum length for the name part of Kubernetes labels (after prefix/)
	// This ensures compliance with Kubernetes label naming rules for prefixed labels
	MaxLabelNameLength = 63

	// Condition types for managed namespaces
	ConditionNamespaceAvailable = "NamespaceAvailable"

	// Condition reasons
	ReasonNamespaceApplied   = "NamespaceApplied"
	ReasonNamespaceApplyFail = "NamespaceApplyFailed"
)

// GetHubClusterSetLabel returns the hub-specific cluster set label for the given hub hash.
// It handles truncation if the hub hash exceeds the maximum label name length.
func GetHubClusterSetLabel(hubHash string) string {
	truncatedHubHash := hubHash
	if len(hubHash) > MaxLabelNameLength {
		truncatedHubHash = hubHash[:MaxLabelNameLength]
	}
	return ClusterSetLabelPrefix + truncatedHubHash
}

// managedNamespaceReconcile manages namespaces on the spoke cluster based on
// the managed namespace configuration from a hub cluster
type managedNamespaceReconcile struct {
	// This label stores the hub hash and is added to a managed namespace with value 'true'.
	// When the namespace becomes unmanaged or the cluster is detached from the hub, the value
	// is set to 'false'. In some edge cases (e.g., when the agent switches to a new hub via
	// bootstrap kubeconfig update), the value may remain 'true', which could lead to cleanup
	// issues if managed namespace deletion is supported in the future.
	hubClusterSetLabel   string
	spokeKubeClient      kubernetes.Interface
	spokeNamespaceLister corev1listers.NamespaceLister
}

// reconcile implements the statusReconcile interface for managed namespace management
func (r *managedNamespaceReconcile) reconcile(ctx context.Context, syncCtx factory.SyncContext, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling managed namespaces", "clusterName", cluster.Name)

	// Skip if cluster is being deleted
	if !cluster.DeletionTimestamp.IsZero() {
		logger.V(4).Info("ManagedCluster is being deleted, cleanup all managed namespaces")
		if err := r.cleanupPreviouslyManagedNamespaces(ctx, syncCtx, sets.Set[string]{}); err != nil {
			logger.Error(err, "Failed to cleanup previously managed namespaces")
			return cluster, reconcileContinue, err
		}
		return cluster, reconcileContinue, nil
	}

	// Skip if no managed namespaces are configured
	if len(cluster.Status.ManagedNamespaces) == 0 {
		logger.V(4).Info("No managed namespaces configured")
		// Still need to cleanup previously managed namespaces
		if err := r.cleanupPreviouslyManagedNamespaces(ctx, syncCtx, sets.Set[string]{}); err != nil {
			logger.Error(err, "Failed to cleanup previously managed namespaces")
			return cluster, reconcileContinue, err
		}
		return cluster, reconcileContinue, nil
	}

	updatedCluster := cluster.DeepCopy()
	var allErrors []error

	// Get current managed namespace names for quick lookup
	currentManagedNS := sets.Set[string]{}
	for _, managedNS := range updatedCluster.Status.ManagedNamespaces {
		currentManagedNS.Insert(managedNS.Name)
	}

	// Process each managed namespace from cluster status
	for i, managedNS := range updatedCluster.Status.ManagedNamespaces {
		nsName := managedNS.Name
		clusterSetName := managedNS.ClusterSet

		logger.V(4).Info("Processing managed namespace", "namespace", nsName, "clusterSet", clusterSetName)

		// Create or update the namespace
		if err := r.createOrUpdateNamespace(ctx, syncCtx, nsName, clusterSetName); err != nil {
			// Update condition: Failed
			condition := metav1.Condition{
				Type:               ConditionNamespaceAvailable,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonNamespaceApplyFail,
				Message:            fmt.Sprintf("Failed to apply namespace: %v", err),
				LastTransitionTime: metav1.Now(),
			}
			meta.SetStatusCondition(&updatedCluster.Status.ManagedNamespaces[i].Conditions, condition)

			logger.Error(err, "Failed to create managed namespace", "namespace", nsName, "clusterSet", clusterSetName)
			allErrors = append(allErrors, fmt.Errorf("failed to create namespace %q: %w", nsName, err))
			continue
		}

		// Update condition: Success
		condition := metav1.Condition{
			Type:               ConditionNamespaceAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonNamespaceApplied,
			Message:            "Namespace successfully applied and managed",
			LastTransitionTime: metav1.Now(),
		}
		meta.SetStatusCondition(&updatedCluster.Status.ManagedNamespaces[i].Conditions, condition)

		logger.V(4).Info("Successfully processed managed namespace", "namespace", nsName, "clusterSet", clusterSetName)
	}

	// Clean up previously managed namespaces by setting label to 'false'
	// Keeps a record of which namespaces were previously managed by this hub
	if err := r.cleanupPreviouslyManagedNamespaces(ctx, syncCtx, currentManagedNS); err != nil {
		logger.Error(err, "Failed to cleanup previously managed namespaces")
		allErrors = append(allErrors, fmt.Errorf("failed to cleanup previously managed namespaces: %w", err))
	}

	// Return aggregated errors from all operations
	aggregatedErr := utilerrors.NewAggregate(allErrors)
	return updatedCluster, reconcileContinue, aggregatedErr
}

// createOrUpdateNamespace creates or updates a namespace
func (r *managedNamespaceReconcile) createOrUpdateNamespace(ctx context.Context, syncCtx factory.SyncContext, nsName, clusterSetName string) error {
	logger := klog.FromContext(ctx)

	// Add hub-specific clusterset label using hub hash
	labels := map[string]string{
		r.hubClusterSetLabel: "true",
	}

	// Create the namespace object
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nsName,
			Labels: labels,
		},
	}

	recorderWrapper := commonrecorder.NewEventsRecorderWrapper(ctx, syncCtx.Recorder())
	_, changed, err := resourceapply.ApplyNamespace(ctx, r.spokeKubeClient.CoreV1(), recorderWrapper, namespace)
	if err != nil {
		return fmt.Errorf("failed to apply namespace %q: %w", nsName, err)
	}

	if changed {
		syncCtx.Recorder().Eventf(ctx, "NamespaceApplied", "Applied managed namespace %q for cluster set %q", nsName, clusterSetName)
		logger.V(4).Info("Applied namespace", "namespace", nsName, "clusterSet", clusterSetName)
	}

	return nil
}

// cleanupPreviouslyManagedNamespaces sets the hub-specific label to 'false' for namespaces
// that are no longer in the current managed namespace list
func (r *managedNamespaceReconcile) cleanupPreviouslyManagedNamespaces(ctx context.Context, syncCtx factory.SyncContext, currentManagedNS sets.Set[string]) error {
	logger := klog.FromContext(ctx)

	// Get all namespaces with our hub-specific label
	selector := labels.SelectorFromSet(labels.Set{r.hubClusterSetLabel: "true"})
	namespaces, err := r.spokeNamespaceLister.List(selector)
	if err != nil {
		return fmt.Errorf("failed to list namespaces with label %s: %w", r.hubClusterSetLabel, err)
	}

	var allErrors []error
	for _, ns := range namespaces {
		// Skip if this namespace is still managed
		if currentManagedNS.Has(ns.Name) {
			continue
		}

		// Set label to 'false' for previously managed namespace
		labels := map[string]string{
			r.hubClusterSetLabel: "false",
		}

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   ns.Name,
				Labels: labels,
			},
		}

		recorderWrapper := commonrecorder.NewEventsRecorderWrapper(ctx, syncCtx.Recorder())
		_, changed, err := resourceapply.ApplyNamespace(ctx, r.spokeKubeClient.CoreV1(), recorderWrapper, namespace)
		if err != nil {
			logger.Error(err, "Failed to update previously managed namespace", "namespace", ns.Name)
			allErrors = append(allErrors, fmt.Errorf("failed to update namespace %q: %w", ns.Name, err))
			continue
		}

		if changed {
			syncCtx.Recorder().Eventf(ctx, "NamespaceUnmanaged", "Set namespace %q as no longer managed", ns.Name)
			logger.V(4).Info("Updated previously managed namespace", "namespace", ns.Name)
		}
	}

	return utilerrors.NewAggregate(allErrors)
}
