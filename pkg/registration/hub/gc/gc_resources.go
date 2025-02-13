package gc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/metadata"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/common/helpers"
)

type gcResourcesController struct {
	metadataClient  metadata.Interface
	resourceGVRList []schema.GroupVersionResource
}

// the range of cleanupPriority is [0,100].
// the resource with the smaller number is deleted first.
type cleanupPriority int

const (
	// the min priority, the resources with this priority will be first deleted.
	minCleanupPriority cleanupPriority = 0
	// the max priority, the resources with this priority will be last deleted.
	maxCleanupPriority cleanupPriority = 100
)

var requeueError = helpers.NewRequeueError("gc requeue", 5*time.Second)

func newGCResourcesController(metadataClient metadata.Interface,
	resourceList []schema.GroupVersionResource) *gcResourcesController {
	return &gcResourcesController{
		metadataClient:  metadataClient,
		resourceGVRList: resourceList,
	}
}

func (r *gcResourcesController) reconcile(ctx context.Context,
	cluster *clusterv1.ManagedCluster, clusterNamespace string) error {
	var errs []error
	// delete the resources in order. to delete the next resource after all resource instances are deleted.
	for _, resourceGVR := range r.resourceGVRList {
		resourceList, err := r.metadataClient.Resource(resourceGVR).
			Namespace(clusterNamespace).List(ctx, metav1.ListOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to list resource %v. err:%v", resourceGVR.Resource, err)
		}
		if len(resourceList.Items) == 0 {
			continue
		}

		if cluster != nil {
			remainingCnt, finalizerPendingCnt := r.RemainingCnt(resourceList)
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:   clusterv1.ManagedClusterConditionDeleting,
				Status: metav1.ConditionFalse,
				Reason: clusterv1.ConditionDeletingReasonResourceRemaining,
				Message: fmt.Sprintf("The resource %v is remaning, the remaining count is %v, "+
					"the finalizer pending count is %v", resourceGVR.Resource, remainingCnt, finalizerPendingCnt),
			})
		}

		// sort the resources by priority, and then find the lowest priority.
		priorityResourceMap := mapPriorityResource(resourceList)
		firstDeletePriority := getFirstDeletePriority(priorityResourceMap)
		// delete the resource instances with the lowest priority in one reconciling.
		for _, resourceName := range priorityResourceMap[firstDeletePriority] {
			err = r.metadataClient.Resource(resourceGVR).Namespace(clusterNamespace).
				Delete(ctx, resourceName, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				errs = append(errs, err)
			}
		}
		if len(errs) != 0 {
			return fmt.Errorf("failed to clean up %v. err:%v", resourceGVR.Resource, utilerrors.NewAggregate(errs))
		}
		return requeueError
	}

	if cluster != nil {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionDeleting,
			Status:  metav1.ConditionTrue,
			Reason:  clusterv1.ConditionDeletingReasonNoResource,
			Message: "No cleaned resource in cluster ns.",
		})
	}
	return nil
}

func mapPriorityResource(resourceList *metav1.PartialObjectMetadataList) map[cleanupPriority][]string {
	priorityResourceMap := map[cleanupPriority][]string{}
	appendResourceFunc := func(priority cleanupPriority, name string) {
		if len(priorityResourceMap[priority]) == 0 {
			priorityResourceMap[priority] = []string{name}
		} else {
			priorityResourceMap[priority] = append(priorityResourceMap[priority], name)
		}
	}

	// the resources which have invalid priority value(not in [0.100]) will be set
	// into minCleanupPriority set and delete first.
	for _, resource := range resourceList.Items {
		priority := getCleanupPriority(resource)
		appendResourceFunc(priority, resource.Name)
	}

	return priorityResourceMap
}

func getFirstDeletePriority(priorityResourceMap map[cleanupPriority][]string) cleanupPriority {
	var firstDeletePriority cleanupPriority = -1
	for priority := range priorityResourceMap {
		if firstDeletePriority == -1 {
			firstDeletePriority = priority
			continue
		}
		if priority < firstDeletePriority {
			firstDeletePriority = priority
		}
	}
	return firstDeletePriority
}

func (r *gcResourcesController) RemainingCnt(
	resourceList *metav1.PartialObjectMetadataList) (remainingCnt, finalizerPendingCnt int) {
	for _, item := range resourceList.Items {
		if len(item.Finalizers) != 0 {
			finalizerPendingCnt++
		}
	}
	return len(resourceList.Items), finalizerPendingCnt
}

// getCleanupPriority is to convert the value of cleanupPriority annotation to a cleanupPriority.
// the range of cleanupPriority is [0,100].
// set cleanupPriority to 0 if there is no cleanup annotation or the value is not an int number or out of the range.
func getCleanupPriority(resource metav1.PartialObjectMetadata) cleanupPriority {
	priorityValue, ok := resource.Annotations[clusterv1.CleanupPriorityAnnotationKey]
	if !ok {
		return minCleanupPriority
	}
	priority, err := strconv.Atoi(priorityValue)
	if err != nil || cleanupPriority(priority) > maxCleanupPriority ||
		cleanupPriority(priority) < minCleanupPriority {
		klog.Warningf("the resource %v has invalid priority value %s.", resource.Name, priorityValue)
		return minCleanupPriority
	}
	return cleanupPriority(priority)
}
