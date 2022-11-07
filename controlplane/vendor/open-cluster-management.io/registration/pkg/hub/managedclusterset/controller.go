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
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	v1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

// managedClusterSetController reconciles instances of ManagedClusterSet on the hub.
type managedClusterSetController struct {
	clusterClient    clientset.Interface
	clusterLister    clusterlisterv1.ManagedClusterLister
	clusterSetLister clusterlisterv1beta1.ManagedClusterSetLister
	eventRecorder    events.Recorder
	queue            workqueue.RateLimitingInterface
}

// NewManagedClusterSetController creates a new managed cluster set controller
func NewManagedClusterSetController(
	clusterClient clientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	recorder events.Recorder) factory.Controller {

	controllerName := "managed-clusterset-controller"
	syncCtx := factory.NewSyncContext(controllerName, recorder)

	c := &managedClusterSetController{
		clusterClient:    clusterClient,
		clusterLister:    clusterInformer.Lister(),
		clusterSetLister: clusterSetInformer.Lister(),
		eventRecorder:    recorder.WithComponentSuffix("managed-cluster-set-controller"),
		queue:            syncCtx.Queue(),
	}
	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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
	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterSetInformer.Informer()).
		WithBareInformers(clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterSetController", recorder)
}

func (c *managedClusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	clusterSetName := syncCtx.QueueKey()
	if len(clusterSetName) == 0 {
		return nil
	}
	klog.V(4).Infof("Reconciling ManagedClusterSet %s", clusterSetName)
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
func (c *managedClusterSetController) syncClusterSet(ctx context.Context, originalClusterSet *clusterv1beta1.ManagedClusterSet) error {
	clusterSet := originalClusterSet.DeepCopy()
	clusters, err := clusterv1beta1.GetClustersFromClusterSet(clusterSet, c.clusterLister)
	if err != nil {
		return err
	}
	count := len(clusters)
	// update clusterset status
	emptyCondition := metav1.Condition{
		Type: clusterv1beta1.ManagedClusterSetConditionEmpty,
	}
	if count == 0 {
		emptyCondition.Status = metav1.ConditionTrue
		emptyCondition.Reason = "NoClusterMatched"
		emptyCondition.Message = "No ManagedCluster selected"
	} else {
		emptyCondition.Status = metav1.ConditionFalse
		emptyCondition.Reason = "ClustersSelected"
		emptyCondition.Message = fmt.Sprintf("%d ManagedClusters selected", count)
	}
	meta.SetStatusCondition(&clusterSet.Status.Conditions, emptyCondition)

	// skip update if cluster set status does not change
	if reflect.DeepEqual(clusterSet.Status.Conditions, originalClusterSet.Status.Conditions) {
		return nil
	}

	_, err = c.clusterClient.ClusterV1beta1().ManagedClusterSets().UpdateStatus(ctx, clusterSet, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update status of ManagedClusterSet %q: %w", clusterSet.Name, err)
	}

	return nil
}

// enqueueClusterClusterSet enqueue a cluster related clusterset
func (c *managedClusterSetController) enqueueClusterClusterSet(cluster *v1.ManagedCluster) {
	clusterSets, err := clusterv1beta1.GetClusterSetsOfCluster(cluster, c.clusterSetLister)
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
	oldClusterSets, err := clusterv1beta1.GetClusterSetsOfCluster(oldCluster, c.clusterSetLister)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error to get GetClusterSetsOfCluster. Error %v", err))
		return
	}
	newClusterSets, err := clusterv1beta1.GetClusterSetsOfCluster(newCluster, c.clusterSetLister)
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
func getDiffClusterSetsNames(oldSets, newSets []*clusterv1beta1.ManagedClusterSet) sets.String {
	oldSetsMap := sets.NewString()
	newSetsMap := sets.NewString()

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
