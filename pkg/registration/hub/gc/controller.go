package gc

import (
	"context"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/metadata"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

var (
	addonGvr = schema.GroupVersionResource{Group: "addon.open-cluster-management.io",
		Version: "v1alpha1", Resource: "managedclusteraddons"}
	workGvr = schema.GroupVersionResource{Group: "work.open-cluster-management.io",
		Version: "v1", Resource: "manifestworks"}
)

type GCController struct {
	clusterLister         clusterv1listers.ManagedClusterLister
	clusterPatcher        patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	gcResourcesController *gcResourcesController
	eventRecorder         events.Recorder
}

// NewGCController ensures the related resources are cleaned up after cluster is deleted
func NewGCController(
	clusterInformer informerv1.ManagedClusterInformer,
	clusterClient clientset.Interface,
	metadataClient metadata.Interface,
	eventRecorder events.Recorder,
	gcResourceList []string,
) factory.Controller {
	clusterPatcher := patcher.NewPatcher[
		*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
		clusterClient.ClusterV1().ManagedClusters())

	controller := &GCController{
		clusterLister:  clusterInformer.Lister(),
		clusterPatcher: clusterPatcher,
		eventRecorder:  eventRecorder.WithComponentSuffix("gc-resources"),
	}
	if len(gcResourceList) != 0 {
		gcResources := []schema.GroupVersionResource{}
		for _, gcResource := range gcResourceList {
			subStrings := strings.Split(gcResource, "/")
			if len(subStrings) != 3 {
				klog.Errorf("invalid gc-resource-list flag: %v", gcResources)
				continue
			}
			gcResources = append(gcResources, schema.GroupVersionResource{
				Group: subStrings[0], Version: subStrings[1], Resource: subStrings[2]})
		}
		controller.gcResourcesController = newGCResourcesController(metadataClient, gcResources)
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterInformer.Informer()).
		WithSync(controller.sync).ToController("GCController", eventRecorder)
}

// gc controller is watching cluster and to do these jobs:
//  1. add a cleanup finalizer to managedCluster if the cluster is not deleting.
//  2. clean up the resources in the cluster ns after the cluster is deleted.
func (r *GCController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterName := controllerContext.QueueKey()
	if clusterName == "" || clusterName == factory.DefaultQueueKey {
		return nil
	}

	cluster, err := r.clusterLister.Get(clusterName)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	var copyCluster *clusterv1.ManagedCluster
	if cluster != nil {
		if cluster.DeletionTimestamp.IsZero() {
			_, err = r.clusterPatcher.AddFinalizer(ctx, cluster, commonhelper.GcFinalizer)
			return err
		}
		copyCluster = cluster.DeepCopy()
	}

	gcErr := r.gcResourcesController.reconcile(ctx, copyCluster, clusterName)
	if cluster == nil {
		return gcErr
	}

	if gcErr != nil && !errors.Is(gcErr, requeueError) {
		r.eventRecorder.Eventf("ResourceCleanupFail",
			"failed to cleanup resources in cluster %s:%v", cluster.Name, gcErr)

		meta.SetStatusCondition(&copyCluster.Status.Conditions, metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionDeleting,
			Status:  metav1.ConditionFalse,
			Reason:  clusterv1.ConditionDeletingReasonResourceError,
			Message: gcErr.Error(),
		})
	}

	if _, err = r.clusterPatcher.PatchStatus(ctx, cluster, copyCluster.Status, cluster.Status); err != nil {
		return err
	}

	if gcErr != nil {
		return gcErr
	}

	r.eventRecorder.Eventf("ResourceCleanupCompleted",
		"resources in cluster %s are cleaned up", cluster.Name)

	return r.clusterPatcher.RemoveFinalizer(ctx, cluster, commonhelper.GcFinalizer)
}
