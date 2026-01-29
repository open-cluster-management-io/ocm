package clusterprofile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
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

	"open-cluster-management.io/ocm/pkg/registration/hub/managedclustersetbinding"
)

const (
	ClusterProfileManagerName = "open-cluster-management"
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
	kubeClient               kubernetes.Interface
	clusterLister            listerv1.ManagedClusterLister
	clusterSetLister         clusterlisterv1beta2.ManagedClusterSetLister
	clusterSetBindingLister  clusterlisterv1beta2.ManagedClusterSetBindingLister
	clusterSetBindingIndexer cache.Indexer
	clusterProfileClient     cpclientset.Interface
	clusterProfileLister     cplisterv1alpha1.ClusterProfileLister
}

// NewClusterProfileLifecycleController creates a controller that manages ClusterProfile lifecycle
func NewClusterProfileLifecycleController(
	kubeClient kubernetes.Interface,
	clusterInformer informerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta2.ManagedClusterSetBindingInformer,
	clusterProfileClient cpclientset.Interface,
	clusterProfileInformer cpinformerv1alpha1.ClusterProfileInformer) factory.Controller {

	// Note: ByClusterSetIndex indexer is already added by managedclustersetbinding controller,
	// so we don't need to add it again here. Informers are shared across controllers.

	c := &clusterProfileLifecycleController{
		kubeClient:               kubeClient,
		clusterLister:            clusterInformer.Lister(),
		clusterSetLister:         clusterSetInformer.Lister(),
		clusterSetBindingLister:  clusterSetBindingInformer.Lister(),
		clusterSetBindingIndexer: clusterSetBindingInformer.Informer().GetIndexer(),
		clusterProfileClient:     clusterProfileClient,
		clusterProfileLister:     clusterProfileInformer.Lister(),
	}

	controller := factory.New().
		WithBareInformers(clusterInformer.Informer(), clusterProfileInformer.Informer()).
		WithInformersQueueKeysFunc(c.clusterSetToQueueKeys, clusterSetInformer.Informer()).
		WithInformersQueueKeysFunc(c.bindingToQueueKey, clusterSetBindingInformer.Informer()).
		WithSync(c.sync).
		ToController("ClusterProfileLifecycleController")

	// Add custom event handlers that only handle create and delete (not update) to avoid noisy events
	c.registerClusterEventHandler(clusterInformer, controller)
	c.registerProfileEventHandler(clusterProfileInformer, controller)

	return controller
}

// registerClusterEventHandler adds a custom event handler to cluster informer that only processes
// create and delete events, skipping updates to avoid noisy events.
func (c *clusterProfileLifecycleController) registerClusterEventHandler(
	clusterInformer informerv1.ManagedClusterInformer,
	controller factory.Controller) {

	queue := controller.SyncContext().Queue()

	_, err := clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cluster, ok := obj.(*v1.ManagedCluster)
			if !ok {
				return
			}
			keys := c.clusterToQueueKeys(cluster)
			for _, key := range keys {
				queue.Add(key)
			}
		},
		// UpdateFunc intentionally omitted - we don't care about managedcluster updates
		DeleteFunc: func(obj interface{}) {
			cluster, ok := obj.(*v1.ManagedCluster)
			if !ok {
				// Handle tombstone
				if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					cluster, ok = tombstone.Obj.(*v1.ManagedCluster)
					if !ok {
						return
					}
				} else {
					return
				}
			}
			keys := c.clusterToQueueKeys(cluster)
			for _, key := range keys {
				queue.Add(key)
			}
		},
	})
	if err != nil {
		utilruntime.HandleError(err)
	}
}

// registerProfileEventHandler adds a custom event handler to profile informer that only processes
// create and delete events, skipping updates to avoid noisy events.
func (c *clusterProfileLifecycleController) registerProfileEventHandler(
	clusterProfileInformer cpinformerv1alpha1.ClusterProfileInformer,
	controller factory.Controller) {

	queue := controller.SyncContext().Queue()

	_, err := clusterProfileInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			profile, ok := obj.(*cpv1alpha1.ClusterProfile)
			if !ok {
				return
			}
			keys := c.profileToQueueKey(profile)
			for _, key := range keys {
				queue.Add(key)
			}
		},
		// UpdateFunc intentionally omitted - we don't care about clusterprofile updates
		DeleteFunc: func(obj interface{}) {
			profile, ok := obj.(*cpv1alpha1.ClusterProfile)
			if !ok {
				// Handle tombstone
				if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					profile, ok = tombstone.Obj.(*cpv1alpha1.ClusterProfile)
					if !ok {
						return
					}
				} else {
					return
				}
			}
			keys := c.profileToQueueKey(profile)
			for _, key := range keys {
				queue.Add(key)
			}
		},
	})
	if err != nil {
		utilruntime.HandleError(err)
	}
}

// getBindingsByClusterSet efficiently retrieves all bindings for a given clusterset using the indexer
func (c *clusterProfileLifecycleController) getBindingsByClusterSet(clusterSetName string) ([]*v1beta2.ManagedClusterSetBinding, error) {
	objs, err := c.clusterSetBindingIndexer.ByIndex(managedclustersetbinding.ByClusterSetIndex, clusterSetName)
	if err != nil {
		return nil, err
	}

	bindings := make([]*v1beta2.ManagedClusterSetBinding, 0, len(objs))
	for _, obj := range objs {
		binding, ok := obj.(*v1beta2.ManagedClusterSetBinding)
		if !ok {
			continue
		}
		bindings = append(bindings, binding)
	}

	return bindings, nil
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

	// Use indexer to efficiently find all bindings that reference this clusterset
	bindings, err := c.getBindingsByClusterSet(clusterSet.Name)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get bindings for clusterset %s: %w", clusterSet.Name, err))
		return nil
	}

	// Collect unique namespaces that have bindings to this clusterset
	namespaces := sets.New[string]()
	for _, binding := range bindings {
		namespaces.Insert(binding.Namespace)
	}

	return namespaces.UnsortedList()
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

	// For each clusterset, use indexer to efficiently find namespaces with bindings to it
	namespaces := sets.New[string]()
	for _, clusterSet := range clusterSets {
		bindings, err := c.getBindingsByClusterSet(clusterSet.Name)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get bindings for clusterset %s: %w", clusterSet.Name, err))
			continue
		}
		for _, binding := range bindings {
			namespaces.Insert(binding.Namespace)
		}
	}

	return namespaces.UnsortedList()
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

	// 0. Check if namespace exists and is not terminating
	ns, err := c.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		logger.V(4).Info("Namespace not found, skipping reconciliation")
		return nil
	}
	if err != nil {
		return err
	}
	if !ns.DeletionTimestamp.IsZero() {
		logger.V(4).Info("Namespace is terminating, skipping reconciliation")
		return nil
	}

	// 1. Get all bindings in this namespace
	allBindings, err := c.clusterSetBindingLister.ManagedClusterSetBindings(namespace).List(labels.Everything())
	if err != nil {
		return err
	}

	logger.V(4).Info("Found bindings", "count", len(allBindings))

	// 2. Build the desired state: which clusters should have profiles in this namespace
	desiredClusters := sets.New[string]()

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

		// Add clusters to desired set
		for _, cluster := range clusters {
			// Skip clusters that are being deleted
			if !cluster.DeletionTimestamp.IsZero() {
				continue
			}
			desiredClusters.Insert(cluster.Name)
		}
	}

	logger.V(4).Info("Calculated desired state", "desiredClusterCount", desiredClusters.Len())

	// 3. Get all existing profiles in this namespace managed by us
	existingProfiles, err := c.clusterProfileLister.ClusterProfiles(namespace).List(
		labels.SelectorFromSet(labels.Set{
			cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
		}))
	if err != nil {
		return err
	}

	// Build set of existing cluster names
	existingClusters := sets.New[string]()
	for _, profile := range existingProfiles {
		existingClusters.Insert(profile.Name)
	}

	logger.V(4).Info("Found existing profiles", "count", existingClusters.Len())

	// 4. Reconcile using set difference operations
	// Clusters to create = desired - existing
	clustersToCreate := desiredClusters.Difference(existingClusters)
	// Clusters to delete = existing - desired
	clustersToDelete := existingClusters.Difference(desiredClusters)

	var errs []error

	// Create missing profiles
	for clusterName := range clustersToCreate {
		err := c.createClusterProfile(ctx, namespace, clusterName)
		if err != nil {
			logger.Error(err, "Failed to create ClusterProfile", "cluster", clusterName)
			errs = append(errs, fmt.Errorf("failed to create ClusterProfile %s/%s: %w", namespace, clusterName, err))
		}
	}

	// Delete extra profiles
	for clusterName := range clustersToDelete {
		// ClusterProfile.Name equals clusterName, so we can delete directly
		err := c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Delete(
			ctx, clusterName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete ClusterProfile", "cluster", clusterName)
			errs = append(errs, fmt.Errorf("failed to delete ClusterProfile %s/%s: %w", namespace, clusterName, err))
		} else if err == nil {
			logger.V(2).Info("Deleted ClusterProfile", "name", clusterName)
		}
	}

	return utilerrors.NewAggregate(errs)
}

// createClusterProfile creates a new ClusterProfile in the specified namespace
func (c *clusterProfileLifecycleController) createClusterProfile(ctx context.Context, namespace, clusterName string) error {
	logger := klog.FromContext(ctx)

	clusterProfile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            clusterName,
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
