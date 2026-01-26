package manifestcontroller

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/objectreader"
)

type appliedManifestWorkReconciler struct {
	spokeDynamicClient dynamic.Interface
	objectReader       objectreader.ObjectReader
	rateLimiter        workqueue.TypedRateLimiter[string]
}

func (m *appliedManifestWorkReconciler) reconcile(
	ctx context.Context,
	_ factory.SyncContext,
	manifestWork *workapiv1.ManifestWork,
	appliedManifestWork *workapiv1.AppliedManifestWork,
	results []applyResult) (*workapiv1.ManifestWork, *workapiv1.AppliedManifestWork, []applyResult, error) {
	logger := klog.FromContext(ctx)
	if !appliedManifestWork.DeletionTimestamp.IsZero() {
		return manifestWork, appliedManifestWork, results, nil
	}

	// In a case where a managed cluster switches to a new hub with the same hub hash, the same manifestworks
	// will be created for this cluster on the new hub without any condition. Once the work agent connects to
	// the new hub, the applied resources of those manifestwork on this managed cluster should not be removed
	// before the manifestworks are applied the first time.
	if appliedCondition := meta.FindStatusCondition(manifestWork.Status.Conditions, workapiv1.WorkApplied); appliedCondition == nil {
		// if the manifestwork has not been applied on the managed cluster yet, do nothing
		return manifestWork, appliedManifestWork, results, nil
	}

	// get the latest applied resources from the manifests in resource status. We get this from status instead of
	// spec because manifests in spec are only resource templates, while resource status records the real resources
	// maintained by the manifest work.
	var appliedResources []workapiv1.AppliedManifestResourceMeta
	var errs []error
	for _, result := range results {
		uid, err := m.getUIDFromResult(ctx, result)
		switch {
		case errors.IsNotFound(err):
			logger.V(2).Info(
				"Resource with key does not exist",
				"resourceNamespace", result.resourceMeta.Namespace, "resourceName", result.resourceMeta.Name)
			continue
		case err != nil:
			errs = append(errs, fmt.Errorf(
				"failed to get resource with key %s/%s: %w",
				result.resourceMeta.Namespace, result.resourceMeta.Name, err))
			continue
		}

		appliedResources = append(appliedResources, workapiv1.AppliedManifestResourceMeta{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     result.resourceMeta.Group,
				Resource:  result.resourceMeta.Resource,
				Namespace: result.resourceMeta.Namespace,
				Name:      result.resourceMeta.Name,
			},
			Version: result.resourceMeta.Version,
			UID:     string(uid),
		})
	}
	if len(errs) != 0 {
		return manifestWork, appliedManifestWork, results, utilerrors.NewAggregate(errs)
	}

	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	// delete applied resources which are no longer maintained by manifest work
	noLongerMaintainedResources := helper.FindUntrackedResources(appliedManifestWork.Status.AppliedResources, appliedResources)

	reason := fmt.Sprintf("it is no longer maintained by manifestwork %s", manifestWork.Name)

	// unregister from objectReader
	objectreader.UnRegisterInformerFromAppliedManifestWork(
		ctx, m.objectReader, appliedManifestWork.Spec.ManifestWorkName, noLongerMaintainedResources)

	resourcesPendingFinalization, errs := helper.DeleteAppliedResources(
		ctx, noLongerMaintainedResources, reason, m.spokeDynamicClient, *owner)
	if len(errs) != 0 {
		return manifestWork, appliedManifestWork, results, utilerrors.NewAggregate(errs)
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

	// update appliedmanifestwork status with latest applied resources. if this conflicts, we'll try again later
	// for retrying update without reassessing the status can cause overwriting of valid information.
	appliedManifestWork.Status.AppliedResources = appliedResources

	// requeue the work if there exists any resource pending for finalization
	if len(resourcesPendingFinalization) != 0 {
		return manifestWork, appliedManifestWork, results, commonhelper.NewRequeueError(
			"requeue due to pending deleting resources",
			m.rateLimiter.When(manifestWork.Name),
		)
	}

	// reset the rate limiter for the manifest work
	if len(resourcesPendingFinalization) == 0 {
		m.rateLimiter.Forget(manifestWork.Name)
	}

	return manifestWork, appliedManifestWork, results, nil
}

// getUIDFromResult get uid of the resource from result at first, call api to get if it is not in the result
func (m *appliedManifestWorkReconciler) getUIDFromResult(ctx context.Context, result applyResult) (types.UID, error) {
	if result.Error == nil {
		accessor, err := meta.Accessor(result.Result)
		if err == nil && accessor.GetUID() != "" {
			return accessor.GetUID(), nil
		}
	}

	gvr := schema.GroupVersionResource{
		Group:    result.resourceMeta.Group,
		Version:  result.resourceMeta.Version,
		Resource: result.resourceMeta.Resource,
	}
	if len(gvr.Resource) == 0 || len(gvr.Version) == 0 || len(result.resourceMeta.Name) == 0 {
		return "", fmt.Errorf("resource with key %s/%s not found", result.resourceMeta.Namespace, result.resourceMeta.Name)
	}

	// get object if cannot get the object from the result.
	u, err := m.spokeDynamicClient.
		Resource(gvr).
		Namespace(result.resourceMeta.Namespace).
		Get(ctx, result.resourceMeta.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return u.GetUID(), nil
}
