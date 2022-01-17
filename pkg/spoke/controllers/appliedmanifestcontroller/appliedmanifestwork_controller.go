package appliedmanifestcontroller

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
)

// AppliedManifestWorkController is to sync the applied resources of appliedmanifestwork with related
// manifestwork and delete any resouce which is no longer maintained by the manifestwork
type AppliedManifestWorkController struct {
	manifestWorkClient        workv1client.ManifestWorkInterface
	manifestWorkLister        worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	appliedManifestWorkLister worklister.AppliedManifestWorkLister
	spokeDynamicClient        dynamic.Interface
	hubHash                   string
	rateLimiter               workqueue.RateLimiter
}

// NewAppliedManifestWorkController returns a AppliedManifestWorkController
func NewAppliedManifestWorkController(
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash string) factory.Controller {

	controller := &AppliedManifestWorkController{
		manifestWorkClient:        manifestWorkClient,
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		spokeDynamicClient:        spokeDynamicClient,
		hubHash:                   hubHash,
		rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithInformersQueueKeyFunc(helper.AppliedManifestworkQueueKeyFunc(hubHash), appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController("AppliedManifestWorkController", recorder)
}

func (m *AppliedManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	// no work to do if we're deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	appliedManifestWorkName := fmt.Sprintf("%s-%s", m.hubHash, manifestWork.Name)
	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	if errors.IsNotFound(err) {
		// appliedmanifestwork not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	// no work to do if we're deleted
	if !appliedManifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	return m.syncManifestWork(ctx, controllerContext, manifestWork, appliedManifestWork)
}

// syncManifestWork collects latest applied resources from ManifestWork.Status.ResourceStatus and merges them with
// existing applied resources in appliedmanifestwork. It also deletes applied resources for stale manifests
func (m *AppliedManifestWorkController) syncManifestWork(
	ctx context.Context,
	controllerContext factory.SyncContext,
	manifestWork *workapiv1.ManifestWork,
	originalAppliedManifestWork *workapiv1.AppliedManifestWork) error {
	appliedManifestWork := originalAppliedManifestWork.DeepCopy()

	// get the latest applied resources from the manifests in resource status. We get this from status instead of
	// spec because manifests in spec are only resource templates, while resource status records the real resources
	// maintained by the manifest work.
	var appliedResources []workapiv1.AppliedManifestResourceMeta
	var errs []error
	for _, resourceStatus := range manifestWork.Status.ResourceStatus.Manifests {
		gvr := schema.GroupVersionResource{Group: resourceStatus.ResourceMeta.Group, Version: resourceStatus.ResourceMeta.Version, Resource: resourceStatus.ResourceMeta.Resource}
		if len(gvr.Resource) == 0 || len(gvr.Version) == 0 || len(resourceStatus.ResourceMeta.Name) == 0 {
			continue
		}

		u, err := m.spokeDynamicClient.
			Resource(gvr).
			Namespace(resourceStatus.ResourceMeta.Namespace).
			Get(context.TODO(), resourceStatus.ResourceMeta.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			klog.V(2).Infof(
				"Resource %v with key %s/%s does not exist",
				gvr, resourceStatus.ResourceMeta.Namespace, resourceStatus.ResourceMeta.Name)
			continue
		}

		if err != nil {
			errs = append(errs, fmt.Errorf(
				"Failed to get resource %v with key %s/%s: %w",
				gvr, resourceStatus.ResourceMeta.Namespace, resourceStatus.ResourceMeta.Name, err))
			continue
		}

		appliedResources = append(appliedResources, workapiv1.AppliedManifestResourceMeta{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     resourceStatus.ResourceMeta.Group,
				Resource:  resourceStatus.ResourceMeta.Resource,
				Namespace: resourceStatus.ResourceMeta.Namespace,
				Name:      resourceStatus.ResourceMeta.Name,
			},
			Version: resourceStatus.ResourceMeta.Version,
			UID:     string(u.GetUID()),
		})
	}
	if len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	// delete applied resources which are no longer maintained by manifest work
	noLongerMaintainedResources := findUntrackedResources(appliedManifestWork.Status.AppliedResources, appliedResources)

	reason := fmt.Sprintf("it is no longer maintained by manifestwork %s", manifestWork.Name)

	resourcesPendingFinalization, errs := helper.DeleteAppliedResources(
		noLongerMaintainedResources, reason, m.spokeDynamicClient, controllerContext.Recorder(), *owner)
	if len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	appliedResources = append(appliedResources, resourcesPendingFinalization...)

	// sort applied resources
	sort.SliceStable(appliedResources, func(i, j int) bool {
		switch {
		case appliedResources[i].Group != appliedResources[j].Group:
			return appliedResources[i].Group < appliedResources[j].Group
		case appliedResources[i].Version != appliedResources[j].Version:
			return appliedResources[i].Version < appliedResources[j].Version
		case appliedResources[i].Resource != appliedResources[j].Resource:
			return appliedResources[i].Resource < appliedResources[j].Resource
		case appliedResources[i].Namespace != appliedResources[j].Namespace:
			return appliedResources[i].Namespace < appliedResources[j].Namespace
		default:
			return appliedResources[i].Name < appliedResources[j].Name
		}
	})

	willSkipStatusUpdate := reflect.DeepEqual(appliedManifestWork.Status.AppliedResources, appliedResources)
	if willSkipStatusUpdate {
		// requeue the work if there exists any resource pending for finalization
		if len(resourcesPendingFinalization) != 0 {
			controllerContext.Queue().AddAfter(manifestWork.Name, m.rateLimiter.When(manifestWork.Name))
		}
		return nil
	}

	// reset the rate limiter for the manifest work
	if len(resourcesPendingFinalization) == 0 {
		m.rateLimiter.Forget(manifestWork.Name)
	}

	// update appliedmanifestwork status with latest applied resources. if this conflicts, we'll try again later
	// for retrying update without reassessing the status can cause overwriting of valid information.
	appliedManifestWork.Status.AppliedResources = appliedResources
	_, err := m.appliedManifestWorkClient.UpdateStatus(ctx, appliedManifestWork, metav1.UpdateOptions{})
	return err
}

// findUntrackedResources returns applied resources which are no longer tracked by manifestwork
// API version should be ignored when checking if a resource is no longer tracked by a manifestwork.
// This is because we treat resources of same GroupResource but different version equivalent.
// It also compares UID of the appliedResources to identify the untracked appliedResources because
// 1. The UID should keep the same for resources with different versions.
// 2. The UID in the newAppliedResources is always the latest updated one. The only possibility that UID
// in appliedResources differs from what in newAppliedResources is that this resource is recreated.
// Its UID in appliedResources is invalid hence recording it as untracked applied resource and delete it is safe.
func findUntrackedResources(appliedResources, newAppliedResources []workapiv1.AppliedManifestResourceMeta) []workapiv1.AppliedManifestResourceMeta {
	var untracked []workapiv1.AppliedManifestResourceMeta

	resourceIndex := map[workapiv1.AppliedManifestResourceMeta]struct{}{}
	for _, resource := range newAppliedResources {
		key := resource.DeepCopy()
		key.UID, key.Version = "", ""
		resourceIndex[*key] = struct{}{}
	}

	for _, resource := range appliedResources {
		key := resource.DeepCopy()
		key.UID, key.Version = "", ""
		if _, ok := resourceIndex[*key]; !ok {
			untracked = append(untracked, resource)
		}
	}

	return untracked
}
