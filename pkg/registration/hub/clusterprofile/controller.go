package clusterprofile

import (
	"context"
	"reflect"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	ClusterProfileManagerName = "open-cluster-management"

	// Indexer names
	bindingsByClusterSet  = "bindingsByClusterSet"
	profilesByClusterName = "profilesByClusterName"
)

// clusterProfileController reconciles instances of ClusterProfile on the hub.
type clusterProfileController struct {
	clusterLister            listerv1.ManagedClusterLister
	clusterSetLister         clusterlisterv1beta2.ManagedClusterSetLister
	clusterSetBindingLister  clusterlisterv1beta2.ManagedClusterSetBindingLister
	clusterProfileClient     cpclientset.Interface
	clusterProfileLister     cplisterv1alpha1.ClusterProfileLister
	clusterSetBindingIndexer cache.Indexer // Index: clusterset name → bindings
	clusterProfileIndexer    cache.Indexer // Index: cluster name → profiles
}

// bindingListerAdapter adapts ManagedClusterSetBindingLister to the SDK's expected interface
type bindingListerAdapter struct {
	lister clusterlisterv1beta2.ManagedClusterSetBindingLister
}

func (a *bindingListerAdapter) List(namespace string, selector labels.Selector) ([]*v1beta2.ManagedClusterSetBinding, error) {
	return a.lister.ManagedClusterSetBindings(namespace).List(selector)
}

// indexBindingByClusterSet indexes ManagedClusterSetBinding by the ClusterSet name
func indexBindingByClusterSet(obj interface{}) ([]string, error) {
	binding, ok := obj.(*v1beta2.ManagedClusterSetBinding)
	if !ok {
		return nil, nil
	}
	return []string{binding.Spec.ClusterSet}, nil
}

// indexProfileByClusterName indexes ClusterProfile by the cluster name (profile name = cluster name)
func indexProfileByClusterName(obj interface{}) ([]string, error) {
	profile, ok := obj.(*cpv1alpha1.ClusterProfile)
	if !ok {
		return nil, nil
	}
	return []string{profile.Name}, nil
}

// NewClusterProfileController creates a new managed cluster controller
func NewClusterProfileController(
	clusterInformer informerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta2.ManagedClusterSetBindingInformer,
	clusterProfileClient cpclientset.Interface,
	clusterProfileInformer cpinformerv1alpha1.ClusterProfileInformer) factory.Controller {

	// Add indexer for ManagedClusterSetBinding by ClusterSet name
	if err := clusterSetBindingInformer.Informer().AddIndexers(cache.Indexers{
		bindingsByClusterSet: indexBindingByClusterSet,
	}); err != nil {
		klog.Errorf("Failed to add indexer for ManagedClusterSetBinding: %v", err)
	}

	// Add indexer for ClusterProfile by cluster name
	if err := clusterProfileInformer.Informer().AddIndexers(cache.Indexers{
		profilesByClusterName: indexProfileByClusterName,
	}); err != nil {
		klog.Errorf("Failed to add indexer for ClusterProfile: %v", err)
	}

	c := &clusterProfileController{
		clusterLister:            clusterInformer.Lister(),
		clusterSetLister:         clusterSetInformer.Lister(),
		clusterSetBindingLister:  clusterSetBindingInformer.Lister(),
		clusterProfileClient:     clusterProfileClient,
		clusterProfileLister:     clusterProfileInformer.Lister(),
		clusterSetBindingIndexer: clusterSetBindingInformer.Informer().GetIndexer(),
		clusterProfileIndexer:    clusterProfileInformer.Informer().GetIndexer(),
	}

	return factory.New().
		WithInformersQueueKeysFunc(c.clusterToQueueKeys, clusterInformer.Informer()).
		WithInformersQueueKeysFunc(c.clusterSetToQueueKeys, clusterSetInformer.Informer()).
		WithInformersQueueKeysFunc(c.bindingToQueueKeys, clusterSetBindingInformer.Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaNamespaceName, clusterProfileInformer.Informer()).
		WithSync(c.sync).
		ToController("ClusterProfileController")
}

// clusterToQueueKeys maps a ManagedCluster to all (namespace, cluster) queue keys
func (c *clusterProfileController) clusterToQueueKeys(obj runtime.Object) []string {
	cluster, ok := obj.(*v1.ManagedCluster)
	if !ok {
		return nil
	}

	keys := []string{}

	// Find all clustersets containing this cluster
	clusterSets, err := clustersdkv1beta2.GetClusterSetsOfCluster(cluster, c.clusterSetLister)
	if err != nil {
		klog.Errorf("Failed to get clustersets for cluster %s: %v", cluster.Name, err)
		return keys
	}

	// For each clusterset, find all bindings to that set
	for _, clusterSet := range clusterSets {
		bindings, err := c.getBindingsByClusterSet(clusterSet.Name)
		if err != nil {
			klog.Errorf("Failed to get bindings for clusterset %s: %v", clusterSet.Name, err)
			continue
		}
		for _, binding := range bindings {
			keys = append(keys, binding.Namespace+"/"+cluster.Name)
		}
	}

	// Also enqueue existing profiles for cleanup
	profiles, err := c.getProfilesByClusterName(cluster.Name)
	if err != nil {
		klog.Errorf("Failed to get profiles for cluster %s: %v", cluster.Name, err)
	} else {
		for _, profile := range profiles {
			keys = append(keys, profile.Namespace+"/"+cluster.Name)
		}
	}

	return keys
}

// clusterSetToQueueKeys maps a ManagedClusterSet to all (namespace, cluster) queue keys
func (c *clusterProfileController) clusterSetToQueueKeys(obj runtime.Object) []string {
	clusterSet, ok := obj.(*v1beta2.ManagedClusterSet)
	if !ok {
		return nil
	}

	keys := []string{}

	// Get all clusters in the set
	clusters, err := clustersdkv1beta2.GetClustersFromClusterSet(clusterSet, c.clusterLister)
	if err != nil {
		klog.Errorf("Failed to get clusters from clusterset %s: %v", clusterSet.Name, err)
		return keys
	}

	// Get all bindings to this set
	bindings, err := c.getBindingsByClusterSet(clusterSet.Name)
	if err != nil {
		klog.Errorf("Failed to get bindings for clusterset %s: %v", clusterSet.Name, err)
		return keys
	}

	// Generate cross-product of (bindings × clusters)
	for _, binding := range bindings {
		for _, cluster := range clusters {
			keys = append(keys, binding.Namespace+"/"+cluster.Name)
		}
	}

	return keys
}

// bindingToQueueKeys maps a ManagedClusterSetBinding to all (namespace, cluster) queue keys
// A ManagedCluster may be referenced by multiple ManagedClusterSets andManagedClusterBindings in the same
// namespace. In this case, only one ClusterProfile is synced, deduplicated by the namespace/name key.
func (c *clusterProfileController) bindingToQueueKeys(obj runtime.Object) []string {
	binding, ok := obj.(*v1beta2.ManagedClusterSetBinding)
	if !ok {
		return nil
	}

	keys := []string{}

	// Get the clusterset
	clusterSet, err := c.clusterSetLister.Get(binding.Spec.ClusterSet)
	if err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("Failed to get clusterset %s: %v", binding.Spec.ClusterSet, err)
		}
		return keys
	}

	// Get all clusters in the set
	clusters, err := clustersdkv1beta2.GetClustersFromClusterSet(clusterSet, c.clusterLister)
	if err != nil {
		klog.Errorf("Failed to get clusters from clusterset %s: %v", clusterSet.Name, err)
		return keys
	}

	// Generate queue keys for each cluster in this namespace
	for _, cluster := range clusters {
		keys = append(keys, binding.Namespace+"/"+cluster.Name)
	}

	return keys
}

func (c *clusterProfileController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	namespace, clusterName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	logger := klog.FromContext(ctx).WithValues("namespace", namespace, "clusterName", clusterName)
	logger.V(4).Info("Reconciling ClusterProfile")

	// Get the cluster (if deleted or deleting → delete profile)
	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) || (cluster != nil && !cluster.DeletionTimestamp.IsZero()) {
		return c.deleteClusterProfile(ctx, namespace, clusterName)
	}
	if err != nil {
		return err
	}

	// Determine if profile should exist
	shouldExist, err := c.shouldClusterProfileExist(cluster, namespace)
	if err != nil {
		return err
	}

	// Get existing profile
	existingProfile, err := c.clusterProfileLister.ClusterProfiles(namespace).Get(clusterName)
	profileExists := err == nil

	// Reconcile based on desired vs actual state
	if !shouldExist && profileExists {
		return c.deleteClusterProfile(ctx, namespace, clusterName)
	}
	if shouldExist && !profileExists {
		return c.createClusterProfile(ctx, syncCtx, namespace, cluster)
	}
	if shouldExist && profileExists {
		return c.updateClusterProfile(ctx, syncCtx, namespace, cluster, existingProfile)
	}

	return nil
}

// shouldClusterProfileExist checks if a ClusterProfile should exist for the given cluster in the given namespace
func (c *clusterProfileController) shouldClusterProfileExist(cluster *v1.ManagedCluster, namespace string) (bool, error) {
	// Get all bound bindings in the namespace using adapter
	adapter := &bindingListerAdapter{lister: c.clusterSetBindingLister}
	boundBindings, err := clustersdkv1beta2.GetBoundManagedClusterSetBindings(namespace, adapter)
	if err != nil {
		return false, err
	}

	// Get all clustersets containing this cluster
	clusterSets, err := clustersdkv1beta2.GetClusterSetsOfCluster(cluster, c.clusterSetLister)
	if err != nil {
		return false, err
	}

	// Check if any binding references a clusterset containing the cluster
	clusterSetNames := make(map[string]bool)
	for _, cs := range clusterSets {
		clusterSetNames[cs.Name] = true
	}

	for _, binding := range boundBindings {
		if clusterSetNames[binding.Spec.ClusterSet] {
			return true, nil
		}
	}

	return false, nil
}

// getBindingsByClusterSet returns all bindings that reference the given clusterset
func (c *clusterProfileController) getBindingsByClusterSet(clusterSetName string) ([]*v1beta2.ManagedClusterSetBinding, error) {
	objs, err := c.clusterSetBindingIndexer.ByIndex(bindingsByClusterSet, clusterSetName)
	if err != nil {
		return nil, err
	}

	bindings := make([]*v1beta2.ManagedClusterSetBinding, 0, len(objs))
	for _, obj := range objs {
		binding, ok := obj.(*v1beta2.ManagedClusterSetBinding)
		if ok {
			bindings = append(bindings, binding)
		}
	}
	return bindings, nil
}

// getProfilesByClusterName returns all profiles for the given cluster across namespaces
func (c *clusterProfileController) getProfilesByClusterName(clusterName string) ([]*cpv1alpha1.ClusterProfile, error) {
	objs, err := c.clusterProfileIndexer.ByIndex(profilesByClusterName, clusterName)
	if err != nil {
		return nil, err
	}

	profiles := make([]*cpv1alpha1.ClusterProfile, 0, len(objs))
	for _, obj := range objs {
		profile, ok := obj.(*cpv1alpha1.ClusterProfile)
		if ok {
			profiles = append(profiles, profile)
		}
	}
	return profiles, nil
}

// createClusterProfile creates a new ClusterProfile
func (c *clusterProfileController) createClusterProfile(ctx context.Context, syncCtx factory.SyncContext, namespace string, cluster *v1.ManagedCluster) error {
	logger := klog.FromContext(ctx)
	clusterProfile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: namespace,
			Labels:    map[string]string{cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: cluster.Name,
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	// Sync labels and status from cluster
	syncLabelsFromCluster(clusterProfile, cluster)
	syncStatusFromCluster(clusterProfile, cluster)

	_, err := c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Create(ctx, clusterProfile, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	logger.V(2).Info("Created ClusterProfile", "namespace", namespace, "name", cluster.Name)
	syncCtx.Recorder().Eventf(ctx, "ClusterProfileCreated", "cluster profile %s/%s created", namespace, cluster.Name)
	return nil
}

// updateClusterProfile updates an existing ClusterProfile
func (c *clusterProfileController) updateClusterProfile(ctx context.Context, syncCtx factory.SyncContext, namespace string, cluster *v1.ManagedCluster, existing *cpv1alpha1.ClusterProfile) error {
	// Skip if not managed by OCM
	if existing.Spec.ClusterManager.Name != ClusterProfileManagerName {
		klog.FromContext(ctx).Info("Not managed by open-cluster-management, skipping", "namespace", namespace, "cluster", cluster.Name)
		return nil
	}

	// Create a patcher for this specific namespace
	profilePatcher := patcher.NewPatcher[
		*cpv1alpha1.ClusterProfile, cpv1alpha1.ClusterProfileSpec, cpv1alpha1.ClusterProfileStatus](
		c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace))

	newProfile := existing.DeepCopy()

	// Sync labels
	syncLabelsFromCluster(newProfile, cluster)

	// Patch labels if modified
	labelsModified := !reflect.DeepEqual(existing.Labels, newProfile.Labels)

	if labelsModified {
		_, err := profilePatcher.PatchLabelAnnotations(ctx, newProfile, newProfile.ObjectMeta, existing.ObjectMeta)
		if err != nil {
			return err
		}
	}

	// Sync status
	syncStatusFromCluster(newProfile, cluster)

	// Patch status
	updated, err := profilePatcher.PatchStatus(ctx, newProfile, newProfile.Status, existing.Status)
	if err != nil {
		return err
	}

	if updated {
		syncCtx.Recorder().Eventf(ctx, "ClusterProfileSynced", "cluster profile %s/%s synced", namespace, cluster.Name)
	}

	return nil
}

// deleteClusterProfile deletes a ClusterProfile if it exists
func (c *clusterProfileController) deleteClusterProfile(ctx context.Context, namespace, clusterName string) error {
	err := c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Delete(ctx, clusterName, metav1.DeleteOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		klog.FromContext(ctx).V(2).Info("Deleted ClusterProfile", "namespace", namespace, "name", clusterName)
	}
	return err
}

// syncLabelsFromCluster syncs required labels from ManagedCluster to ClusterProfile
func syncLabelsFromCluster(profile *cpv1alpha1.ClusterProfile, cluster *v1.ManagedCluster) {
	mclLabels := cluster.GetLabels()
	mclSetLabel := mclLabels[v1beta2.ClusterSetLabel]

	requiredLabels := map[string]string{
		cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
		cpv1alpha1.LabelClusterSetKey:     mclSetLabel,
	}

	modified := false
	resourcemerge.MergeMap(&modified, &profile.Labels, requiredLabels)
}

// syncStatusFromCluster syncs status fields from ManagedCluster to ClusterProfile
func syncStatusFromCluster(profile *cpv1alpha1.ClusterProfile, cluster *v1.ManagedCluster) {
	// Sync version
	profile.Status.Version.Kubernetes = cluster.Status.Version.Kubernetes

	// Sync properties from cluster claims
	cpProperties := []cpv1alpha1.Property{}
	for _, claim := range cluster.Status.ClusterClaims {
		cpProperties = append(cpProperties, cpv1alpha1.Property{Name: claim.Name, Value: claim.Value})
	}
	profile.Status.Properties = cpProperties

	// Sync conditions
	if availableCondition := meta.FindStatusCondition(cluster.Status.Conditions, v1.ManagedClusterConditionAvailable); availableCondition != nil {
		meta.SetStatusCondition(&profile.Status.Conditions, metav1.Condition{
			Type:    cpv1alpha1.ClusterConditionControlPlaneHealthy,
			Status:  availableCondition.Status,
			Reason:  availableCondition.Reason,
			Message: availableCondition.Message,
		})
	}

	if joinedCondition := meta.FindStatusCondition(cluster.Status.Conditions, v1.ManagedClusterConditionJoined); joinedCondition != nil {
		meta.SetStatusCondition(&profile.Status.Conditions, metav1.Condition{
			Type:    "Joined",
			Status:  joinedCondition.Status,
			Reason:  joinedCondition.Reason,
			Message: joinedCondition.Message,
		})
	}
}
