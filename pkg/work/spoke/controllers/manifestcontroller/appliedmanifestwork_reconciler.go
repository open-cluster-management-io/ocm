package manifestcontroller

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/openshift/library-go/pkg/controller/factory"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/work/helper"
)

type appliedManifestWorkReconciler struct {
	spokeDynamicClient dynamic.Interface
	rateLimiter        workqueue.RateLimiter
}

func (m *appliedManifestWorkReconciler) reconcile(
	ctx context.Context,
	controllerContext factory.SyncContext,
	manifestWork *workapiv1.ManifestWork,
	appliedManifestWork *workapiv1.AppliedManifestWork) (*workapiv1.ManifestWork, *workapiv1.AppliedManifestWork, error) {
	if !appliedManifestWork.DeletionTimestamp.IsZero() {
		return manifestWork, appliedManifestWork, nil
	}

	// In a case where a managed cluster switches to a new hub with the same hub hash, the same manifestworks
	// will be created for this cluster on the new hub without any condition. Once the work agent connects to
	// the new hub, the applied resources of those manifestwork on this managed cluster should not be removed
	// before the manifestworks are applied the first time.
	if appliedCondition := meta.FindStatusCondition(manifestWork.Status.Conditions, workapiv1.WorkApplied); appliedCondition == nil {
		// if the manifestwork has not been applied on the managed cluster yet, do nothing
		return manifestWork, appliedManifestWork, nil
	}

	// get the latest applied resources from the manifests in resource status. We get this from status instead of
	// spec because manifests in spec are only resource templates, while resource status records the real resources
	// maintained by the manifest work.
	var appliedResources []workapiv1.AppliedManifestResourceMeta
	var errs []error
	for _, resourceStatus := range manifestWork.Status.ResourceStatus.Manifests {
		gvr := schema.GroupVersionResource{
			Group:    resourceStatus.ResourceMeta.Group,
			Version:  resourceStatus.ResourceMeta.Version,
			Resource: resourceStatus.ResourceMeta.Resource,
		}
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
				"failed to get resource %v with key %s/%s: %w",
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
		return manifestWork, appliedManifestWork, utilerrors.NewAggregate(errs)
	}

	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	// delete applied resources which are no longer maintained by manifest work
	noLongerMaintainedResources := helper.FindUntrackedResources(appliedManifestWork.Status.AppliedResources, appliedResources)

	reason := fmt.Sprintf("it is no longer maintained by manifestwork %s", manifestWork.Name)

	resourcesPendingFinalization, errs := helper.DeleteAppliedResources(
		ctx, noLongerMaintainedResources, reason, m.spokeDynamicClient, controllerContext.Recorder(), *owner)
	if len(errs) != 0 {
		return manifestWork, appliedManifestWork, utilerrors.NewAggregate(errs)
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
		var err error
		if len(resourcesPendingFinalization) != 0 {
			err = commonhelper.NewRequeueError(
				"requeue due to pending deleting resources",
				m.rateLimiter.When(manifestWork.Name),
			)
		}
		return manifestWork, appliedManifestWork, err
	}

	// reset the rate limiter for the manifest work
	if len(resourcesPendingFinalization) == 0 {
		m.rateLimiter.Forget(manifestWork.Name)
	}

	// update appliedmanifestwork status with latest applied resources. if this conflicts, we'll try again later
	// for retrying update without reassessing the status can cause overwriting of valid information.
	appliedManifestWork.Status.AppliedResources = appliedResources
	return manifestWork, appliedManifestWork, nil
}
