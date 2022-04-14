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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
)

// managedClusterSetController reconciles instances of ManagedClusterSet on the hub.
type managedClusterSetController struct {
	clusterClient    clientset.Interface
	clusterLister    clusterlisterv1.ManagedClusterLister
	clusterSetLister clusterlisterv1beta1.ManagedClusterSetLister
	eventRecorder    events.Recorder
}

// NewManagedClusterSetController creates a new managed cluster set controller
func NewManagedClusterSetController(
	clusterClient clientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	recorder events.Recorder) factory.Controller {

	controllerName := "managed-clusterset-controller"
	syncCtx := factory.NewSyncContext(controllerName, recorder)
	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if clusterSetName := getClusterSet(obj); len(clusterSetName) > 0 {
				syncCtx.Queue().Add(clusterSetName)
				return
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldClusterSetName, newClusterSetName := getClusterSet(oldObj), getClusterSet(newObj)
			if oldClusterSetName == newClusterSetName {
				return
			}

			if len(oldClusterSetName) > 0 {
				syncCtx.Queue().Add(oldClusterSetName)
			}

			if len(newClusterSetName) > 0 {
				syncCtx.Queue().Add(newClusterSetName)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var clusterSetName string
			switch t := obj.(type) {
			case *clusterapiv1.ManagedCluster:
				clusterSetName = getClusterSet(obj)
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(metav1.Object); !ok {
					utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
					return
				}
				clusterSetName = getClusterSet(t.Obj)
			default:
				utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
				return
			}

			if len(clusterSetName) > 0 {
				syncCtx.Queue().Add(clusterSetName)
			}
		},
	})

	c := &managedClusterSetController{
		clusterClient:    clusterClient,
		clusterLister:    clusterInformer.Lister(),
		clusterSetLister: clusterSetInformer.Lister(),
		eventRecorder:    recorder.WithComponentSuffix(controllerName),
	}
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
	klog.Infof("Reconciling ManagedClusterSet %s", clusterSetName)

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
		return fmt.Errorf("failed to sync ManagedClusterSet %q: %w", clusterSetName, err)
	}
	return nil
}

// syncClusterSet syncs a particular cluster set. Currently only support the legacy type of clusterset.
// The logic should be updated once any new type of clusterset added.
func (c *managedClusterSetController) syncClusterSet(ctx context.Context, clusterSet *clusterv1beta1.ManagedClusterSet) error {
	// ignore the non-legacy clusterset
	selectorType := clusterSet.Spec.ClusterSelector.SelectorType
	if len(selectorType) > 0 && selectorType != clusterv1beta1.LegacyClusterSetLabel {
		return nil
	}

	clusterSetCopy := clusterSet.DeepCopy()

	// find out the containing clusters of clusterset
	selector := labels.SelectorFromSet(labels.Set{
		clusterSetLabel: clusterSetCopy.Name,
	})
	clusters, err := c.clusterLister.List(selector)
	if err != nil {
		return fmt.Errorf("failed to list ManagedClusters: %w", err)
	}

	// update clusterset status
	emptyCondition := metav1.Condition{
		Type: clusterv1beta1.ManagedClusterSetConditionEmpty,
	}
	if count := len(clusters); count == 0 {
		emptyCondition.Status = metav1.ConditionTrue
		emptyCondition.Reason = "NoClusterMatched"
		emptyCondition.Message = "No ManagedCluster selected"
	} else {
		emptyCondition.Status = metav1.ConditionFalse
		emptyCondition.Reason = "ClustersSelected"
		emptyCondition.Message = fmt.Sprintf("%d ManagedClusters selected", count)
	}
	meta.SetStatusCondition(&clusterSetCopy.Status.Conditions, emptyCondition)

	// skip update if cluster set status does not change
	if reflect.DeepEqual(clusterSetCopy.Status.Conditions, clusterSet.Status.Conditions) {
		return nil
	}

	_, err = c.clusterClient.ClusterV1beta1().ManagedClusterSets().UpdateStatus(ctx, clusterSetCopy, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update status of ManagedClusterSet %q: %w", clusterSetCopy.Name, err)
	}

	return nil
}

func getClusterSet(obj interface{}) string {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error to get accessor of object: %v", obj))
		return ""
	}
	labels := accessor.GetLabels()
	if len(labels) == 0 {
		return ""
	}

	if clusterSetName := labels[clusterSetLabel]; len(clusterSetName) > 0 {
		return clusterSetName
	}

	return ""
}
