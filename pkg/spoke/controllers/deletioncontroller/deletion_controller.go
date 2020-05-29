package deletioncontroller

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

// StaleManifestDeletionController is to reconcile the applied resources of manifest work and
// delete any resouce which is no longer maintained by the manifest work
type StaleManifestDeletionController struct {
	manifestWorkClient workv1client.ManifestWorkInterface
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	spokeDynamicClient dynamic.Interface
}

// NewStaleManifestDeletionController returns a StaleManifestDeletionController
func NewStaleManifestDeletionController(
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister) factory.Controller {

	controller := &StaleManifestDeletionController{
		manifestWorkClient: manifestWorkClient,
		manifestWorkLister: manifestWorkLister,
		spokeDynamicClient: spokeDynamicClient,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController("StaleManifestDeletionController", recorder)
}

func (m *StaleManifestDeletionController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work  not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	return m.syncManifestWork(ctx, manifestWork)
}

func (m *StaleManifestDeletionController) syncManifestWork(ctx context.Context, originalManifestWork *workapiv1.ManifestWork) error {
	manifestWork := originalManifestWork.DeepCopy()

	// no work to do if we're deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// get the latest applied resources from the manifests in resource status. We get this from status instead of
	// spec because manifests in spec are only resource templates, while resource status records the real resources
	// maintained by the manifest work.
	var appliedResources []workapiv1.AppliedManifestResourceMeta
	for _, resourceStatus := range manifestWork.Status.ResourceStatus.Manifests {
		gvr := schema.GroupVersionResource{Group: resourceStatus.ResourceMeta.Group, Version: resourceStatus.ResourceMeta.Version, Resource: resourceStatus.ResourceMeta.Resource}
		if len(gvr.Resource) == 0 || len(gvr.Version) == 0 || len(resourceStatus.ResourceMeta.Name) == 0 {
			continue
		}

		appliedResources = append(appliedResources, workapiv1.AppliedManifestResourceMeta{
			Group:     resourceStatus.ResourceMeta.Group,
			Version:   resourceStatus.ResourceMeta.Version,
			Resource:  resourceStatus.ResourceMeta.Resource,
			Namespace: resourceStatus.ResourceMeta.Namespace,
			Name:      resourceStatus.ResourceMeta.Name,
		})
	}

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

	// no work to do if applied resources are not changed
	if reflect.DeepEqual(manifestWork.Status.AppliedResources, appliedResources) {
		return nil
	}

	// delete applied resources which are no longer maintained by manifest work
	noLongerMaintainedResources := findUntrackedResources(manifestWork.Status.AppliedResources, appliedResources)
	if errs := m.deleteAppliedResources(noLongerMaintainedResources); len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	// update work status with latest applied resources. if this conflicts, we'll simply try again later
	// for retrying update without reassessing the status can cause overwriting of valid information.
	manifestWork.Status.AppliedResources = appliedResources
	_, err := m.manifestWorkClient.UpdateStatus(ctx, manifestWork, metav1.UpdateOptions{})
	return err
}

// deleteAppliedResources deletes all applied resources in the given resource meta array.
func (m *StaleManifestDeletionController) deleteAppliedResources(resources []workapiv1.AppliedManifestResourceMeta) []error {
	var errs []error
	for _, resource := range resources {
		gvr := schema.GroupVersionResource{Group: resource.Group, Version: resource.Version, Resource: resource.Resource}
		err := m.spokeDynamicClient.Resource(gvr).Namespace(resource.Namespace).Delete(context.TODO(), resource.Name, metav1.DeleteOptions{})
		switch {
		case errors.IsNotFound(err):
			// no-oop
		case err != nil:
			errs = append(errs, fmt.Errorf("Failed to delete resource %v with key %s/%s: %w", gvr, resource.Namespace, resource.Name, err))
			continue
		}
		klog.V(2).Infof("Successfully delete resource %v with key %s/%s", gvr, resource.Namespace, resource.Name)
	}

	return errs
}

// findUntrackedResources returns applied resources which are no longer tracked by manifestwork
func findUntrackedResources(appliedResources, newAppliedResources []workapiv1.AppliedManifestResourceMeta) []workapiv1.AppliedManifestResourceMeta {
	var untracked []workapiv1.AppliedManifestResourceMeta

	resourceIndex := map[workapiv1.AppliedManifestResourceMeta]struct{}{}
	for _, resource := range newAppliedResources {
		resourceIndex[resource] = struct{}{}
	}

	for _, resource := range appliedResources {
		if _, ok := resourceIndex[resource]; !ok {
			untracked = append(untracked, resource)
		}
	}

	return untracked
}
