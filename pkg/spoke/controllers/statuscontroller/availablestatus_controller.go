package statuscontroller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
)

// ControllerSyncInterval is exposed so that integration tests can crank up the controller resync speed.
var ControllerReSyncInterval = 30 * time.Second

// AvailableStatusController is to update the available status conditions of both manifests and manifestworks.
type AvailableStatusController struct {
	manifestWorkClient workv1client.ManifestWorkInterface
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	spokeDynamicClient dynamic.Interface
}

// NewAvailableStatusController returns a AvailableStatusController
func NewAvailableStatusController(
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
) factory.Controller {
	controller := &AvailableStatusController{
		manifestWorkClient: manifestWorkClient,
		manifestWorkLister: manifestWorkLister,
		spokeDynamicClient: spokeDynamicClient,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(ControllerReSyncInterval).ToController("AvailableStatusController", recorder)
}

func (c *AvailableStatusController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	if manifestWorkName != "key" {
		// sync a particular manifestwork
		manifestWork, err := c.manifestWorkLister.Get(manifestWorkName)
		if errors.IsNotFound(err) {
			// work not found, could have been deleted, do nothing.
			return nil
		}
		if err != nil {
			return fmt.Errorf("unable to fetch manifestwork %q: %w", manifestWorkName, err)
		}

		err = c.syncManifestWork(ctx, manifestWork)
		if err != nil {
			return fmt.Errorf("unable to sync manifestwork %q: %w", manifestWork.Name, err)
		}
		return nil
	}

	// resync all manifestworks
	klog.V(4).Infof("Reconciling all ManifestWorks")
	manifestWorks, err := c.manifestWorkLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("unable to list manifestworks: %w", err)
	}

	errs := []error{}
	for _, manifestWork := range manifestWorks {
		err = c.syncManifestWork(ctx, manifestWork)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to sync manifestwork %q: %w", manifestWork.Name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("unable to resync manifestworks: %w", utilerrors.NewAggregate(errs))
	}
	return nil
}

func (c *AvailableStatusController) syncManifestWork(ctx context.Context, originalManifestWork *workapiv1.ManifestWork) error {
	klog.V(4).Infof("Reconciling ManifestWork %q", originalManifestWork.Name)
	manifestWork := originalManifestWork.DeepCopy()

	needStatusUpdate := false
	// handle status condition of manifests
	for index, manifest := range manifestWork.Status.ResourceStatus.Manifests {
		availableStatusCondition := buildAvailableStatusCondition(manifest.ResourceMeta, c.spokeDynamicClient)
		newConditions := helper.MergeStatusConditions(manifest.Conditions, []metav1.Condition{availableStatusCondition})
		if !reflect.DeepEqual(manifestWork.Status.ResourceStatus.Manifests[index].Conditions, newConditions) {
			manifestWork.Status.ResourceStatus.Manifests[index].Conditions = newConditions
			needStatusUpdate = true
		}
	}

	// handle status condition of manifestwork
	var workStatusConditions []metav1.Condition
	switch {
	case len(manifestWork.Status.ResourceStatus.Manifests) == 0:
		// remove condition with type Available if no Manifests exists
		for _, condition := range manifestWork.Status.Conditions {
			if condition.Type != string(workapiv1.WorkAvailable) {
				workStatusConditions = append(workStatusConditions, condition)
			}
		}
	default:
		// aggregate ManifestConditions and update work status condition
		workAvailableStatusCondition := aggregateManifestConditions(manifestWork.Status.ResourceStatus.Manifests)
		workStatusConditions = helper.MergeStatusConditions(manifestWork.Status.Conditions, []metav1.Condition{workAvailableStatusCondition})
	}
	manifestWork.Status.Conditions = workStatusConditions

	// no work if the status of manifestwork does not change
	if !needStatusUpdate && reflect.DeepEqual(originalManifestWork.Status.Conditions, manifestWork.Status.Conditions) {
		return nil
	}

	// update status of manifestwork. if this conflicts, try again later
	_, err := c.manifestWorkClient.UpdateStatus(ctx, manifestWork, metav1.UpdateOptions{})
	return err
}

// aggregateManifestConditions aggregates status conditions of manifests and returns a status
// condition for manifestwork
func aggregateManifestConditions(manifests []workapiv1.ManifestCondition) metav1.Condition {
	available, unavailable, unknown := 0, 0, 0
	for _, manifest := range manifests {
		for _, condition := range manifest.Conditions {
			if condition.Type != string(workapiv1.ManifestAvailable) {
				continue
			}

			switch condition.Status {
			case metav1.ConditionTrue:
				available += 1
			case metav1.ConditionFalse:
				unavailable += 1
			case metav1.ConditionUnknown:
				unknown += 1
			}
		}
	}

	switch {
	case unavailable > 0:
		return metav1.Condition{
			Type:    string(workapiv1.WorkAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  "ResourcesNotAvailable",
			Message: fmt.Sprintf("%d of %d resources are not available", unavailable, len(manifests)),
		}
	case unknown > 0:
		return metav1.Condition{
			Type:    string(workapiv1.WorkAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ResourcesStatusUnknown",
			Message: fmt.Sprintf("%d of %d resources have unknown status", unknown, len(manifests)),
		}
	default:
		return metav1.Condition{
			Type:    string(workapiv1.WorkAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ResourcesAvailable",
			Message: "All resources are available",
		}
	}
}

// buildAvailableStatusCondition returns a StatusCondition with type Available for a given manifest resource
func buildAvailableStatusCondition(resourceMeta workapiv1.ManifestResourceMeta, dynamicClient dynamic.Interface) metav1.Condition {
	conditionType := string(workapiv1.ManifestAvailable)

	if len(resourceMeta.Resource) == 0 || len(resourceMeta.Version) == 0 || len(resourceMeta.Name) == 0 {
		return metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  "IncompletedResourceMeta",
			Message: "Resource meta is incompleted",
		}
	}

	available, err := isResourceAvailable(resourceMeta.Namespace, resourceMeta.Name, schema.GroupVersionResource{
		Group:    resourceMeta.Group,
		Version:  resourceMeta.Version,
		Resource: resourceMeta.Resource,
	}, dynamicClient)
	if err != nil {
		return metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  "FetchingResourceFailed",
			Message: fmt.Sprintf("Failed to fetch resource: %v", err),
		}
	}

	if available {
		return metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "ResourceAvailable",
			Message: "Resource is available",
		}
	}

	return metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  "ResourceNotAvailable",
		Message: "Resource is not available",
	}
}

// isResourceAvailable checks if the specific resource is available or not
func isResourceAvailable(namespace, name string, gvr schema.GroupVersionResource, dynamicClient dynamic.Interface) (bool, error) {
	_, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
