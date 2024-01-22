package addon

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	addOnFeaturePrefix     = "feature.open-cluster-management.io/addon-"
	addOnStatusAvailable   = "available"
	addOnStatusUnhealthy   = "unhealthy"
	addOnStatusUnreachable = "unreachable"
)

// addOnFeatureDiscoveryController monitors ManagedCluster and its ManagedClusterAddOns on hub and
// create/update/delete labels of the ManagedCluster to reflect the status of addons.
type addOnFeatureDiscoveryController struct {
	patcher       patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	clusterLister clusterv1listers.ManagedClusterLister
	addOnLister   addonlisterv1alpha1.ManagedClusterAddOnLister
	recorder      events.Recorder
}

// NewAddOnFeatureDiscoveryController returns an instance of addOnFeatureDiscoveryController
func NewAddOnFeatureDiscoveryController(
	clusterClient clientset.Interface,
	clusterInformer clusterv1informer.ManagedClusterInformer,
	addOnInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &addOnFeatureDiscoveryController{
		patcher: patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			clusterClient.ClusterV1().ManagedClusters()),
		clusterLister: clusterInformer.Lister(),
		addOnLister:   addOnInformers.Lister(),
		recorder:      recorder,
	}

	return factory.New().
		WithInformersQueueKeysFunc(
			queue.QueueKeyByMetaName,
			clusterInformer.Informer()).
		WithInformersQueueKeysFunc(
			queue.QueueKeyByMetaNamespace,
			addOnInformers.Informer()).
		WithSync(c.sync).
		ToController("AddOnFeatureDiscoveryController", recorder)
}

func (c *addOnFeatureDiscoveryController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()

	switch {
	case queueKey == factory.DefaultQueueKey:
		// no need to resync
		return nil
	default:
		// sync the cluster
		return c.syncCluster(ctx, queueKey)
	}
}

func (c *addOnFeatureDiscoveryController) syncCluster(ctx context.Context, clusterName string) error {
	// sync all addon labels on the managed cluster
	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		// cluster is deleted
		return nil
	}
	if err != nil {
		return err
	}

	// Do not update addon label if cluster is deleting
	if !cluster.DeletionTimestamp.IsZero() {
		return nil
	}

	// Do not update if cluster has no available condition yet.
	if meta.FindStatusCondition(cluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable) == nil {
		return nil
	}

	// build labels for existing addons
	addOnLabels := map[string]string{}
	addOns, err := c.addOnLister.ManagedClusterAddOns(clusterName).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("unable to list addOns of cluster %q: %w", clusterName, err)
	}
	for _, addOn := range addOns {
		// addon is deleting
		if !addOn.DeletionTimestamp.IsZero() {
			continue
		}
		key := fmt.Sprintf("%s%s", addOnFeaturePrefix, addOn.Name)
		addOnLabels[key] = getAddOnLabelValue(addOn)
	}

	// remove addon lable if its corresponding addon no longer exists
	for key := range cluster.Labels {
		if !strings.HasPrefix(key, addOnFeaturePrefix) {
			continue
		}

		if _, ok := addOnLabels[key]; !ok {
			addOnLabels[fmt.Sprintf("%s-", key)] = ""
		}
	}

	// merge labels
	modified := false
	newCluster := cluster.DeepCopy()
	resourcemerge.MergeMap(&modified, &newCluster.Labels, addOnLabels)

	// no work if the cluster labels have no change
	if !modified {
		return nil
	}

	// otherwise, update cluster
	_, err = c.patcher.PatchLabelAnnotations(ctx, newCluster, newCluster.ObjectMeta, cluster.ObjectMeta)
	return err
}

func getAddOnLabelValue(addOn *addonv1alpha1.ManagedClusterAddOn) string {
	availableCondition := meta.FindStatusCondition(addOn.Status.Conditions, addonv1alpha1.ManagedClusterAddOnConditionAvailable)
	if availableCondition == nil {
		return addOnStatusUnreachable
	}

	switch availableCondition.Status {
	case metav1.ConditionTrue:
		return addOnStatusAvailable
	case metav1.ConditionFalse:
		return addOnStatusUnhealthy
	default:
		return addOnStatusUnreachable
	}
}
