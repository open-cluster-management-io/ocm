package clusterprofile

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpinformerv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions/apis/v1alpha1"
	cplisterv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/listers/apis/v1alpha1"

	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	byClusterName = "by-cluster-name"
)

// clusterProfileStatusController updates ClusterProfile status and labels from ManagedCluster
// Queue key: cluster name (e.g., "cluster1")
type clusterProfileStatusController struct {
	clusterLister         listerv1.ManagedClusterLister
	clusterProfileClient  cpclientset.Interface
	clusterProfileLister  cplisterv1alpha1.ClusterProfileLister
	clusterProfileIndexer cache.Indexer
}

// indexByClusterName is the indexer function for ClusterProfile by cluster name
func indexByClusterName(obj interface{}) ([]string, error) {
	profile, ok := obj.(*cpv1alpha1.ClusterProfile)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ClusterProfile, but is %T", obj)
	}

	// Index by the cluster-name label
	if clusterName, ok := profile.Labels[v1.ClusterNameLabelKey]; ok {
		return []string{clusterName}, nil
	}

	// Fallback: use profile name as cluster name (lifecycle controller sets Name = cluster name)
	return []string{profile.Name}, nil
}

func NewClusterProfileStatusController(
	clusterInformer informerv1.ManagedClusterInformer,
	clusterProfileClient cpclientset.Interface,
	clusterProfileInformer cpinformerv1alpha1.ClusterProfileInformer) factory.Controller {

	// Add indexer for efficient lookup of profiles by cluster name
	err := clusterProfileInformer.Informer().AddIndexers(cache.Indexers{
		byClusterName: indexByClusterName,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	c := &clusterProfileStatusController{
		clusterLister:         clusterInformer.Lister(),
		clusterProfileClient:  clusterProfileClient,
		clusterProfileLister:  clusterProfileInformer.Lister(),
		clusterProfileIndexer: clusterProfileInformer.Informer().GetIndexer(),
	}

	return factory.New().
		WithInformersQueueKeysFunc(c.clusterToQueueKey, clusterInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			c.profileToQueueKey,
			queue.UnionFilter(
				queue.FileterByLabel(v1.ClusterNameLabelKey),
				queue.FileterByLabelKeyValue(cpv1alpha1.LabelClusterManagerKey, ClusterProfileManagerName),
			),
			clusterProfileInformer.Informer()).
		WithSync(c.sync).
		ToController("ClusterProfileStatusController")
}

func (c *clusterProfileStatusController) clusterToQueueKey(obj runtime.Object) []string {
	cluster, ok := obj.(*v1.ManagedCluster)
	if !ok {
		return nil
	}
	return []string{cluster.Name}
}

func (c *clusterProfileStatusController) profileToQueueKey(obj runtime.Object) []string {
	profile, ok := obj.(*cpv1alpha1.ClusterProfile)
	if !ok {
		return nil
	}

	// Use cluster-name label (filtering already done by WithFilteredEventsInformersQueueKeysFunc)
	if clusterName, ok := profile.Labels[v1.ClusterNameLabelKey]; ok {
		return []string{clusterName}
	}

	// Fallback: profile name = cluster name
	return []string{profile.Name}
}

func (c *clusterProfileStatusController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	clusterName := key
	logger := klog.FromContext(ctx).WithValues("managedCluster", clusterName)

	logger.V(4).Info("Updating ClusterProfile status")

	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		logger.V(4).Info("Cluster not found, skipping status update")
		return nil
	}
	if err != nil {
		return err
	}

	// Use indexer for efficient lookup instead of listing all profiles across all namespaces
	allProfiles, err := c.getProfilesByClusterName(clusterName)
	if err != nil {
		return err
	}

	logger.V(4).Info("Found profiles to update", "count", len(allProfiles))

	updatedCount := 0
	var errs []error
	for _, profile := range allProfiles {
		if profile.Spec.ClusterManager.Name != ClusterProfileManagerName {
			continue
		}

		err := c.updateClusterProfile(ctx, profile, cluster)
		if err != nil {
			logger.Error(err, "Failed to update profile",
				"namespace", profile.Namespace, "name", profile.Name)
			errs = append(errs, fmt.Errorf("failed to update ClusterProfile %s/%s: %w", profile.Namespace, profile.Name, err))
			continue
		}
		updatedCount++
	}

	if updatedCount > 0 {
		logger.V(2).Info("Updated profiles", "count", updatedCount)
		syncCtx.Recorder().Eventf(ctx, "ClusterProfileStatusUpdated",
			"updated %d cluster profiles for cluster %s", updatedCount, clusterName)
	}

	return utilerrors.NewAggregate(errs)
}

// getProfilesByClusterName efficiently retrieves all profiles for a given cluster using the indexer
func (c *clusterProfileStatusController) getProfilesByClusterName(clusterName string) ([]*cpv1alpha1.ClusterProfile, error) {
	objs, err := c.clusterProfileIndexer.ByIndex(byClusterName, clusterName)
	if err != nil {
		return nil, err
	}

	profiles := make([]*cpv1alpha1.ClusterProfile, 0, len(objs))
	for _, obj := range objs {
		profile, ok := obj.(*cpv1alpha1.ClusterProfile)
		if !ok {
			continue
		}
		profiles = append(profiles, profile)
	}

	return profiles, nil
}

func (c *clusterProfileStatusController) updateClusterProfile(
	ctx context.Context,
	existing *cpv1alpha1.ClusterProfile,
	cluster *v1.ManagedCluster) error {

	// Create a patcher for this specific namespace
	profilePatcher := patcher.NewPatcher[
		*cpv1alpha1.ClusterProfile, cpv1alpha1.ClusterProfileSpec, cpv1alpha1.ClusterProfileStatus](
		c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(existing.Namespace))

	newProfile := existing.DeepCopy()

	// Sync status
	syncStatusFromCluster(newProfile, cluster)

	// Patch status first to avoid ResourceVersion conflict
	// If status has been updated, return early - labels will be updated in next reconcile
	updated, err := profilePatcher.PatchStatus(ctx, newProfile, newProfile.Status, existing.Status)
	if updated {
		return err
	}

	// Status wasn't updated, now safe to patch labels
	syncLabelsFromCluster(newProfile, cluster)

	// Patch labels (patcher handles change detection)
	_, err = profilePatcher.PatchLabelAnnotations(ctx, newProfile, newProfile.ObjectMeta, existing.ObjectMeta)

	return err
}

func syncLabelsFromCluster(profile *cpv1alpha1.ClusterProfile, cluster *v1.ManagedCluster) {
	mclLabels := cluster.GetLabels()
	mclSetLabel := mclLabels[v1beta2.ClusterSetLabel]

	requiredLabels := map[string]string{
		cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
		cpv1alpha1.LabelClusterSetKey:     mclSetLabel,
		// Keep the cluster-name label that lifecycle controller added
		v1.ClusterNameLabelKey: cluster.Name,
	}

	modified := false
	resourcemerge.MergeMap(&modified, &profile.Labels, requiredLabels)
}

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
