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

	gcListPageSize int64 = 500
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
		result, err := r.collectGCTargets(ctx, resourceGVR, clusterNamespace)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to list resource %v. err:%v", resourceGVR.Resource, err)
		}
		if result.totalCount == 0 {
			continue
		}

		if cluster != nil {
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:   clusterv1.ManagedClusterConditionDeleting,
				Status: metav1.ConditionFalse,
				Reason: clusterv1.ConditionDeletingReasonResourceRemaining,
				Message: fmt.Sprintf("The resource %v is remaning, the remaining count is %v, "+
					"the finalizer pending count is %v", resourceGVR.Resource, result.totalCount, result.finalizerPendingCount),
			})
		}

		// delete the resource instances with the lowest priority in one reconciling.
		for _, resourceName := range result.lowestPriorityNames {
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

type gcPageResult struct {
	totalCount            int
	finalizerPendingCount int
	lowestPriority        cleanupPriority
	lowestPriorityNames   []string
}

// collectGCTargets paginates through all resources, processing each page as it
// arrives to avoid holding the full resource set in memory. Only resource names
// for the lowest cleanup priority are retained.
func (r *gcResourcesController) collectGCTargets(ctx context.Context,
	gvr schema.GroupVersionResource, namespace string) (*gcPageResult, error) {
	result := &gcPageResult{lowestPriority: -1}
	listOpts := metav1.ListOptions{Limit: gcListPageSize}
	for {
		page, err := r.metadataClient.Resource(gvr).Namespace(namespace).List(ctx, listOpts)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			result.totalCount++
			if len(item.Finalizers) != 0 {
				result.finalizerPendingCount++
			}
			p := getCleanupPriority(item)
			switch {
			case result.lowestPriority == -1 || p < result.lowestPriority:
				result.lowestPriority = p
				result.lowestPriorityNames = []string{item.Name}
			case p == result.lowestPriority:
				result.lowestPriorityNames = append(result.lowestPriorityNames, item.Name)
			}
		}
		if page.Continue == "" {
			break
		}
		listOpts.Continue = page.Continue
	}
	return result, nil
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
