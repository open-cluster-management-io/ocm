package addon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
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
	clusterClient clientset.Interface
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
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		addOnLister:   addOnInformers.Lister(),
		recorder:      recorder,
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			clusterInformer.Informer()).
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			},
			addOnInformers.Informer()).
		WithSync(c.sync).
		ResyncEvery(10*time.Minute).
		ToController("AddOnFeatureDiscoveryController", recorder)
}

func (c *addOnFeatureDiscoveryController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// The value of queueKey might be
	// 1) equal to the default queuekey. It is triggered by resync every 10 minutes;
	// 2) in format: namespace/name. It indicates the event source is a ManagedClusterAddOn;
	// 3) in format: name. It indicates the event source is a ManagedCluster;
	queueKey := syncCtx.QueueKey()
	namespace, name, err := cache.SplitMetaNamespaceKey(queueKey)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	switch {
	case queueKey == factory.DefaultQueueKey:
		// handle resync
		clusters, err := c.clusterLister.List(labels.Everything())
		if err != nil {
			return err
		}

		for _, cluster := range clusters {
			syncCtx.Queue().Add(cluster.Name)
		}
		return nil
	case len(namespace) > 0:
		// sync a particular addon
		return c.syncAddOn(ctx, namespace, name)
	default:
		// sync the cluster
		return c.syncCluster(ctx, name)
	}
}

func (c *addOnFeatureDiscoveryController) syncAddOn(ctx context.Context, clusterName, addOnName string) error {
	klog.V(4).Infof("Reconciling addOn %q", addOnName)

	labels := map[string]string{}
	addOn, err := c.addOnLister.ManagedClusterAddOns(clusterName).Get(addOnName)
	switch {
	case errors.IsNotFound(err):
		// addon is deleted
		key := fmt.Sprintf("%s%s-", addOnFeaturePrefix, addOnName)
		labels[key] = ""
	case err != nil:
		return err
	case !addOn.DeletionTimestamp.IsZero():
		key := fmt.Sprintf("%s%s-", addOnFeaturePrefix, addOnName)
		labels[key] = ""
	default:
		key := fmt.Sprintf("%s%s", addOnFeaturePrefix, addOn.Name)
		labels[key] = getAddOnLabelValue(addOn)
	}

	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		// no cluster, it could be deleted
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to find cluster with name %q: %w", clusterName, err)
	}
	// no work if cluster is deleting
	if !cluster.DeletionTimestamp.IsZero() {
		return nil
	}

	// merge labels
	modified := false
	cluster = cluster.DeepCopy()
	resourcemerge.MergeMap(&modified, &cluster.Labels, labels)

	// no work if the cluster labels have no change
	if !modified {
		return nil
	}

	// otherwise, update cluster
	_, err = c.clusterClient.ClusterV1().ManagedClusters().Update(ctx, cluster, metav1.UpdateOptions{})
	return err
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
	cluster = cluster.DeepCopy()
	resourcemerge.MergeMap(&modified, &cluster.Labels, addOnLabels)

	// no work if the cluster labels have no change
	if !modified {
		return nil
	}

	// otherwise, update cluster
	_, err = c.clusterClient.ClusterV1().ManagedClusters().Update(ctx, cluster, metav1.UpdateOptions{})
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
