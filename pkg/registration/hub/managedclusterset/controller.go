package managedclusterset

import (
	"context"
	"fmt"
	"reflect"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	v1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clustersdkv1beta2 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	// TODO move these to api repos
	ReasonClusterSelected   = "ClustersSelected"
	ReasonNoClusterMatchced = "NoClusterMatched"
)

// managedClusterSetController reconciles instances of ManagedClusterSet on the hub.
type managedClusterSetController struct {
	patcher          patcher.Patcher[*clusterv1beta2.ManagedClusterSet, clusterv1beta2.ManagedClusterSetSpec, clusterv1beta2.ManagedClusterSetStatus]
	clusterLister    clusterlisterv1.ManagedClusterLister
	clusterSetLister clusterlisterv1beta2.ManagedClusterSetLister
	eventRecorder    events.Recorder
	queue            workqueue.RateLimitingInterface
}

// NewManagedClusterSetController creates a new managed cluster set controller
func NewManagedClusterSetController(
	clusterClient clientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	recorder events.Recorder) factory.Controller {

	controllerName := "managed-clusterset-controller"
	syncCtx := factory.NewSyncContext(controllerName, recorder)

	c := &managedClusterSetController{
		patcher: patcher.NewPatcher[
			*clusterv1beta2.ManagedClusterSet, clusterv1beta2.ManagedClusterSetSpec, clusterv1beta2.ManagedClusterSetStatus](
			clusterClient.ClusterV1beta2().ManagedClusterSets()),
		clusterLister:    clusterInformer.Lister(),
		clusterSetLister: clusterSetInformer.Lister(),
		eventRecorder:    recorder.WithComponentSuffix("managed-cluster-set-controller"),
		queue:            syncCtx.Queue(),
	}

	_, err := clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cluster, ok := obj.(*v1.ManagedCluster)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error to get object: %v", obj))
				return
			}
			// enqueue all cluster related clustersets
			c.enqueueClusterClusterSet(cluster)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			// only need handle label update
			oldCluster, ok := oldObj.(*v1.ManagedCluster)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error to get object: %v", oldObj))
				return
			}
			newCluster, ok := newObj.(*v1.ManagedCluster)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error to get object: %v", newObj))
				return
			}
			if reflect.DeepEqual(oldCluster.Labels, newCluster.Labels) {
				return
			}
			c.enqueueUpdateClusterClusterSet(oldCluster, newCluster)
		},
		DeleteFunc: func(obj interface{}) {
			switch t := obj.(type) {
			case *v1.ManagedCluster:
				// enqueue all cluster related clustersets
				c.enqueueClusterClusterSet(t)
			case cache.DeletedFinalStateUnknown:
				cluster, ok := t.Obj.(*v1.ManagedCluster)
				if !ok {
					utilruntime.HandleError(fmt.Errorf("error to get object: %v", obj))
					return
				}
				// enqueue all cluster related clustersets
				c.enqueueClusterClusterSet(cluster)
			default:
				utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
				return
			}
		},
	})

	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterSetInformer.Informer()).
		WithBareInformers(clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterSetController", recorder)
}

func (c *managedClusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	clusterSetName := syncCtx.QueueKey()
	if len(clusterSetName) == 0 {
		return nil
	}
	logger.V(4).Info("Reconciling ManagedClusterSet", "clusterSetName", clusterSetName)
	clusterSet, err := c.clusterSetLister.Get(clusterSetName)
	if errors.IsNotFound(err) {
		// cluster set not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// no work to do if the cluster set is deleted
	if !clusterSet.DeletionTimestamp.IsZero() {
		return nil
	}

	if err := c.syncClusterSet(ctx, clusterSet); err != nil {
		return fmt.Errorf("failed to sync ManagedClusterSet %q: %w", clusterSet.Name, err)
	}

	return nil
}

// syncClusterSet syncs a particular cluster set
func (c *managedClusterSetController) syncClusterSet(ctx context.Context, originalClusterSet *clusterv1beta2.ManagedClusterSet) error {
	clusterSet := originalClusterSet.DeepCopy()
	clusters, err := clustersdkv1beta2.GetClustersFromClusterSet(clusterSet, c.clusterLister)
	if err != nil {
		return err
	}
	count := len(clusters)
	// update clusterset status
	emptyCondition := metav1.Condition{
		Type: clusterv1beta2.ManagedClusterSetConditionEmpty,
	}
	if count == 0 {
		emptyCondition.Status = metav1.ConditionTrue
		emptyCondition.Reason = ReasonNoClusterMatchced
		emptyCondition.Message = "No ManagedCluster selected"
	} else {
		emptyCondition.Status = metav1.ConditionFalse
		emptyCondition.Reason = ReasonClusterSelected
		emptyCondition.Message = fmt.Sprintf("%d ManagedClusters selected", count)
	}
	meta.SetStatusCondition(&clusterSet.Status.Conditions, emptyCondition)

	_, err = c.patcher.PatchStatus(ctx, clusterSet, clusterSet.Status, originalClusterSet.Status)
	if err != nil {
		return fmt.Errorf("failed to update status of ManagedClusterSet %q: %w", clusterSet.Name, err)
	}

	return nil
}

// enqueueClusterClusterSet enqueue a cluster related clusterset
func (c *managedClusterSetController) enqueueClusterClusterSet(cluster *v1.ManagedCluster) {
	clusterSets, err := clustersdkv1beta2.GetClusterSetsOfCluster(cluster, c.clusterSetLister)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error to get GetClusterSetsOfCluster. Error %v", err))
		return
	}
	for _, clusterSet := range clusterSets {
		c.queue.Add(clusterSet.Name)
	}
}

// enqueueUpdateClusterClusterSet get the oldCluster related clustersets and newCluster related clustersets,
// then enqueue the diff clustersets(added clustersets and removed clustersets)
func (c *managedClusterSetController) enqueueUpdateClusterClusterSet(oldCluster, newCluster *v1.ManagedCluster) {
	oldClusterSets, err := clustersdkv1beta2.GetClusterSetsOfCluster(oldCluster, c.clusterSetLister)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error to get GetClusterSetsOfCluster. Error %v", err))
		return
	}
	newClusterSets, err := clustersdkv1beta2.GetClusterSetsOfCluster(newCluster, c.clusterSetLister)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error to get GetClusterSetsOfCluster. Error %v", err))
		return
	}
	diffClusterSets := getDiffClusterSetsNames(oldClusterSets, newClusterSets)

	for diffSet := range diffClusterSets {
		c.queue.Add(diffSet)
	}
}

// getDiffClusterSetsNames return the diff clustersets names
func getDiffClusterSetsNames(oldSets, newSets []*clusterv1beta2.ManagedClusterSet) sets.Set[string] {
	oldSetsMap := sets.New[string]()
	newSetsMap := sets.New[string]()

	for _, oldSet := range oldSets {
		oldSetsMap.Insert(oldSet.Name)
	}

	for _, newSet := range newSets {
		newSetsMap.Insert(newSet.Name)
	}

	diffOldSets := oldSetsMap.Difference(newSetsMap)
	diffNewSets := newSetsMap.Difference(oldSetsMap)

	return diffOldSets.Union(diffNewSets)
}
