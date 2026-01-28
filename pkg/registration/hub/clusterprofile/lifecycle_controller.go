package clusterprofile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpinformerv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions/apis/v1alpha1"
	cplisterv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/listers/apis/v1alpha1"

	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clustersdkv1beta2 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

const (
	ClusterProfileManagerName               = "open-cluster-management"
	ClusterProfileForManagedClusterLabelKey = "open-cluster-management.io/cluster-name"
)

// clusterProfileLifecycleController manages ClusterProfile creation and deletion
// based on ManagedClusterSetBindings.
//
// Queue key: namespace (e.g., "kueue-system")
//
// This controller reconciles ALL ClusterProfiles in a namespace based on ALL
// ManagedClusterSetBindings in that namespace.
//
// Key constraint: ManagedClusterSetBinding.Name MUST equal ManagedClusterSetBinding.Spec.ClusterSet
type clusterProfileLifecycleController struct {
	clusterLister           listerv1.ManagedClusterLister
	clusterSetLister        clusterlisterv1beta2.ManagedClusterSetLister
	clusterSetBindingLister clusterlisterv1beta2.ManagedClusterSetBindingLister
	clusterProfileClient    cpclientset.Interface
	clusterProfileLister    cplisterv1alpha1.ClusterProfileLister
}

// NewClusterProfileLifecycleController creates a controller that manages ClusterProfile lifecycle
func NewClusterProfileLifecycleController(
	clusterInformer informerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta2.ManagedClusterSetBindingInformer,
	clusterProfileClient cpclientset.Interface,
	clusterProfileInformer cpinformerv1alpha1.ClusterProfileInformer) factory.Controller {

	c := &clusterProfileLifecycleController{
		clusterLister:           clusterInformer.Lister(),
		clusterSetLister:        clusterSetInformer.Lister(),
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		clusterProfileClient:    clusterProfileClient,
		clusterProfileLister:    clusterProfileInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeysFunc(c.clusterToQueueKeys, clusterInformer.Informer()).
		WithInformersQueueKeysFunc(c.clusterSetToQueueKeys, clusterSetInformer.Informer()).
		WithInformersQueueKeysFunc(c.bindingToQueueKey, clusterSetBindingInformer.Informer()).
		WithInformersQueueKeysFunc(c.profileToQueueKey, clusterProfileInformer.Informer()).
		WithSync(c.sync).
		ToController("ClusterProfileLifecycleController")
}

// bindingToQueueKey maps a ManagedClusterSetBinding to its namespace
func (c *clusterProfileLifecycleController) bindingToQueueKey(obj runtime.Object) []string {
	binding, ok := obj.(*v1beta2.ManagedClusterSetBinding)
	if !ok {
		return nil
	}
	// Queue the namespace where the binding exists
	return []string{binding.Namespace}
}

// clusterSetToQueueKeys maps a ManagedClusterSet to all namespaces that have bindings to it
func (c *clusterProfileLifecycleController) clusterSetToQueueKeys(obj runtime.Object) []string {
	clusterSet, ok := obj.(*v1beta2.ManagedClusterSet)
	if !ok {
		return nil
	}

	// Find all bindings that reference this clusterset
	// Constraint: binding.Name == binding.Spec.ClusterSet
	allBindings, err := c.clusterSetBindingLister.List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to list bindings for clusterset %s: %w", clusterSet.Name, err))
		return nil
	}

	// Collect unique namespaces that have bindings to this clusterset
	namespaces := make(map[string]bool)
	for _, binding := range allBindings {
		if binding.Spec.ClusterSet == clusterSet.Name {
			namespaces[binding.Namespace] = true
		}
	}

	keys := make([]string, 0, len(namespaces))
	for ns := range namespaces {
		keys = append(keys, ns)
	}
	return keys
}

// clusterToQueueKeys maps a ManagedCluster to all namespaces that should have its profile
func (c *clusterProfileLifecycleController) clusterToQueueKeys(obj runtime.Object) []string {
	cluster, ok := obj.(*v1.ManagedCluster)
	if !ok {
		return nil
	}

	// Find all clustersets containing this cluster
	clusterSets, err := clustersdkv1beta2.GetClusterSetsOfCluster(cluster, c.clusterSetLister)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get clustersets for cluster %s: %w", cluster.Name, err))
		return nil
	}

	// List all bindings
	allBindings, err := c.clusterSetBindingLister.List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to list bindings: %w", err))
		return nil
	}

	// For each clusterset, find namespaces with bindings to it
	namespaces := make(map[string]bool)
	for _, clusterSet := range clusterSets {
		for _, binding := range allBindings {
			if binding.Spec.ClusterSet == clusterSet.Name {
				namespaces[binding.Namespace] = true
			}
		}
	}

	keys := make([]string, 0, len(namespaces))
	for ns := range namespaces {
		keys = append(keys, ns)
	}
	return keys
}

// profileToQueueKey maps a ClusterProfile to its namespace
func (c *clusterProfileLifecycleController) profileToQueueKey(obj runtime.Object) []string {
	profile, ok := obj.(*cpv1alpha1.ClusterProfile)
	if !ok {
		return nil
	}
	return []string{profile.Namespace}
}

// sync reconciles all ClusterProfiles in a given namespace
//
// 1. List all ManagedClusterSetBindings in the namespace
// 2. For each bound binding, get its clusterset and all clusters in it
// 3. Build desired state: set of clusters that should have profiles
// 4. Compare with existing profiles and create/delete as needed
func (c *clusterProfileLifecycleController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	namespace := key // Queue key is the namespace
	logger := klog.FromContext(ctx).WithValues("namespace", namespace)

	logger.V(4).Info("Reconciling ClusterProfiles in namespace")

	// 1. Get all bindings in this namespace
	allBindings, err := c.clusterSetBindingLister.ManagedClusterSetBindings(namespace).List(labels.Everything())
	if err != nil {
		return err
	}

	logger.V(4).Info("Found bindings", "count", len(allBindings))

	// 2. Build the desired state: which clusters should have profiles in this namespace
	// Map: clusterName -> true
	desiredClusters := make(map[string]bool)

	for _, binding := range allBindings {
		// Check if binding is bound
		isBound := meta.IsStatusConditionTrue(binding.Status.Conditions, v1beta2.ClusterSetBindingBoundType)
		if !isBound {
			logger.V(4).Info("Binding not bound, skipping", "binding", binding.Name)
			continue
		}

		// Get the clusterset
		// Note: binding.Name should equal binding.Spec.ClusterSet (constraint)
		clusterSet, err := c.clusterSetLister.Get(binding.Spec.ClusterSet)
		if errors.IsNotFound(err) {
			logger.V(4).Info("ClusterSet not found for binding", "binding", binding.Name, "clusterset", binding.Spec.ClusterSet)
			continue
		}
		if err != nil {
			logger.Error(err, "Failed to get clusterset", "clusterset", binding.Spec.ClusterSet)
			continue
		}

		// Get all clusters in this clusterset
		clusters, err := clustersdkv1beta2.GetClustersFromClusterSet(clusterSet, c.clusterLister)
		if err != nil {
			logger.Error(err, "Failed to get clusters from clusterset", "clusterset", clusterSet.Name)
			continue
		}

		logger.V(4).Info("Processing clusterset",
			"binding", binding.Name,
			"clusterset", clusterSet.Name,
			"clusterCount", len(clusters))

		// Mark these clusters as desired
		for _, cluster := range clusters {
			// Skip clusters that are being deleted
			if !cluster.DeletionTimestamp.IsZero() {
				continue
			}
			desiredClusters[cluster.Name] = true
		}
	}

	logger.V(4).Info("Calculated desired state", "desiredClusterCount", len(desiredClusters))

	// 3. Get all existing profiles in this namespace managed by us
	existingProfiles, err := c.clusterProfileLister.ClusterProfiles(namespace).List(
		labels.SelectorFromSet(labels.Set{
			cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
		}))
	if err != nil {
		return err
	}

	existingClusters := make(map[string]*cpv1alpha1.ClusterProfile)
	for _, profile := range existingProfiles {
		existingClusters[profile.Name] = profile
	}

	logger.V(4).Info("Found existing profiles", "count", len(existingClusters))

	// 4. Reconcile: Create missing, delete extra
	profilesCreated := 0
	profilesDeleted := 0

	// Create missing profiles
	for clusterName := range desiredClusters {
		if _, exists := existingClusters[clusterName]; !exists {
			// Create profile
			err := c.createClusterProfile(ctx, namespace, clusterName)
			if err != nil {
				logger.Error(err, "Failed to create ClusterProfile", "cluster", clusterName)
				utilruntime.HandleError(fmt.Errorf("failed to create ClusterProfile %s/%s: %w", namespace, clusterName, err))
				// Continue with others - don't fail entire sync
			} else {
				profilesCreated++
			}
		}
	}

	// Delete extra profiles (profiles that shouldn't exist)
	for clusterName, profile := range existingClusters {
		if !desiredClusters[clusterName] {
			// Delete profile
			err := c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Delete(
				ctx, profile.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "Failed to delete ClusterProfile", "cluster", clusterName)
				utilruntime.HandleError(fmt.Errorf("failed to delete ClusterProfile %s/%s: %w", namespace, clusterName, err))
			} else if err == nil {
				profilesDeleted++
				logger.V(2).Info("Deleted ClusterProfile", "namespace", namespace, "name", clusterName)
			}
		}
	}

	if profilesCreated > 0 || profilesDeleted > 0 {
		logger.Info("Namespace reconciliation complete",
			"profilesCreated", profilesCreated,
			"profilesDeleted", profilesDeleted,
			"totalDesired", len(desiredClusters))
		syncCtx.Recorder().Eventf(ctx, "ClusterProfilesReconciled",
			"reconciled namespace %s: created %d, deleted %d profiles",
			namespace, profilesCreated, profilesDeleted)
	}

	return nil
}

// createClusterProfile creates a new ClusterProfile in the specified namespace
func (c *clusterProfileLifecycleController) createClusterProfile(ctx context.Context, namespace, clusterName string) error {
	logger := klog.FromContext(ctx)

	clusterProfile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey:       ClusterProfileManagerName,
				ClusterProfileForManagedClusterLabelKey: clusterName,
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: clusterName,
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
		// Note: ClusterProfile Status will be handled by the status controller
	}

	_, err := c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Create(ctx, clusterProfile, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	logger.V(2).Info("Created ClusterProfile", "namespace", namespace, "name", clusterName)
	return nil
}
