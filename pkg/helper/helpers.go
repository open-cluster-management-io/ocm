package helper

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	// unknownKind is returned by resourcehelper.GuessObjectGroupVersionKind() when it
	// cannot tell the kind of the given object
	unknownKind = "<unknown>"
)

var (
	genericScheme = runtime.NewScheme()
)

func init() {
	// add apiextensions v1beta1 to scheme to support CustomResourceDefinition v1beta1
	_ = apiextensionsv1beta1.AddToScheme(genericScheme)
	_ = apiextensionsv1.AddToScheme(genericScheme)
}

// MergeManifestConditions return a new ManifestCondition array which merges the existing manifest
// conditions and the new manifest conditions. Rules to match ManifestCondition between two arrays:
// 1. match the manifest condition with the whole ManifestResourceMeta;
// 2. if not matched, try to match with properties other than ordinal in ManifestResourceMeta
// If no existing manifest condition is matched, the new manifest condition will be used.
func MergeManifestConditions(conditions, newConditions []workapiv1.ManifestCondition) []workapiv1.ManifestCondition {
	merged := []workapiv1.ManifestCondition{}

	// build search indices
	metaIndex := map[workapiv1.ManifestResourceMeta]workapiv1.ManifestCondition{}
	metaWithoutOridinalIndex := map[workapiv1.ManifestResourceMeta]workapiv1.ManifestCondition{}

	duplicated := []workapiv1.ManifestResourceMeta{}
	for _, condition := range conditions {
		metaIndex[condition.ResourceMeta] = condition
		if metaWithoutOridinal := resetOrdinal(condition.ResourceMeta); metaWithoutOridinal != (workapiv1.ManifestResourceMeta{}) {
			if _, exists := metaWithoutOridinalIndex[metaWithoutOridinal]; exists {
				duplicated = append(duplicated, metaWithoutOridinal)
			} else {
				metaWithoutOridinalIndex[metaWithoutOridinal] = condition
			}
		}
	}

	// remove metaWithoutOridinal from index if it is not unique
	for _, metaWithoutOridinal := range duplicated {
		delete(metaWithoutOridinalIndex, metaWithoutOridinal)
	}

	// try to match and merge manifest conditions
	for _, newCondition := range newConditions {
		// match with ResourceMeta
		condition, ok := metaIndex[newCondition.ResourceMeta]

		// match with properties in ResourceMeta other than ordinal if not found yet
		if !ok {
			condition, ok = metaWithoutOridinalIndex[resetOrdinal(newCondition.ResourceMeta)]
		}

		// if there is existing condition, merge it with new condition
		if ok {
			merged = append(merged, mergeManifestCondition(condition, newCondition))
			continue
		}

		// otherwise use the new condition
		for i := range newCondition.Conditions {
			newCondition.Conditions[i].LastTransitionTime = metav1.NewTime(time.Now())
		}

		merged = append(merged, newCondition)
	}

	return merged
}

func resetOrdinal(meta workapiv1.ManifestResourceMeta) workapiv1.ManifestResourceMeta {
	return workapiv1.ManifestResourceMeta{
		Group:     meta.Group,
		Version:   meta.Version,
		Kind:      meta.Kind,
		Resource:  meta.Resource,
		Name:      meta.Name,
		Namespace: meta.Namespace,
	}
}

func mergeManifestCondition(condition, newCondition workapiv1.ManifestCondition) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: newCondition.ResourceMeta,
		//Note this func is only used for merging status conditions, the statusFeedbacks should keep the old one.
		StatusFeedbacks: condition.StatusFeedbacks,
		Conditions:      MergeStatusConditions(condition.Conditions, newCondition.Conditions),
	}
}

// MergeStatusConditions returns a new status condition array with merged status conditions. It is based on newConditions,
// and merges the corresponding existing conditions if exists.
func MergeStatusConditions(conditions []metav1.Condition, newConditions []metav1.Condition) []metav1.Condition {
	merged := []metav1.Condition{}

	for _, condition := range conditions {
		merged = append(merged, condition)
	}

	for _, condition := range newConditions {
		// merge two conditions if necessary
		meta.SetStatusCondition(&merged, condition)
	}

	return merged
}

type UpdateManifestWorkStatusFunc func(status *workapiv1.ManifestWorkStatus) error

func UpdateManifestWorkStatus(
	ctx context.Context,
	client workv1client.ManifestWorkInterface,
	manifestWork *workapiv1.ManifestWork,
	updateFuncs ...UpdateManifestWorkStatusFunc) (*workapiv1.ManifestWorkStatus, bool, error) {
	// in order to reduce the number of GET requests to hub apiserver, try to update the manifestwork
	// fetched from informer cache (with lister).
	updatedWorkStatus, updated, err := updateManifestWorkStatus(ctx, client, manifestWork, updateFuncs...)
	if err == nil {
		return updatedWorkStatus, updated, nil
	}

	// if the update failed, retry with the manifestwork resource fetched with work client.
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		manifestWork, err := client.Get(ctx, manifestWork.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		updatedWorkStatus, updated, err = updateManifestWorkStatus(ctx, client, manifestWork, updateFuncs...)
		return err
	})

	return updatedWorkStatus, updated, err
}

// updateManifestWorkStatus updates the status of the given manifestWork. The manifestWork is mutated.
func updateManifestWorkStatus(
	ctx context.Context,
	client workv1client.ManifestWorkInterface,
	manifestWork *workapiv1.ManifestWork,
	updateFuncs ...UpdateManifestWorkStatusFunc) (*workapiv1.ManifestWorkStatus, bool, error) {
	oldStatus := &manifestWork.Status
	newStatus := oldStatus.DeepCopy()
	for _, update := range updateFuncs {
		if err := update(newStatus); err != nil {
			return nil, false, err
		}
	}
	if equality.Semantic.DeepEqual(oldStatus, newStatus) {
		// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
		return newStatus, false, nil
	}

	manifestWork.Status = *newStatus
	updatedManifestWork, err := client.UpdateStatus(ctx, manifestWork, metav1.UpdateOptions{})
	if err != nil {
		return nil, false, err
	}
	return &updatedManifestWork.Status, true, nil
}

// DeleteAppliedResources deletes all given applied resources and returns those pending for finalization
// If the uid recorded in resources is different from what we get by client, ignore the deletion.
func DeleteAppliedResources(
	resources []workapiv1.AppliedManifestResourceMeta,
	reason string,
	dynamicClient dynamic.Interface,
	recorder events.Recorder,
	owner metav1.OwnerReference) ([]workapiv1.AppliedManifestResourceMeta, []error) {
	var resourcesPendingFinalization []workapiv1.AppliedManifestResourceMeta
	var errs []error

	// set owner to be removed
	ownerCopy := owner.DeepCopy()
	ownerCopy.UID = types.UID(fmt.Sprintf("%s-", owner.UID))

	// We hard coded the delete policy to Background
	// TODO: reivist if user needs to set other options. Setting to Orphan may not make sense, since when
	// the manifestwork is removed, there is no way to track the orphaned resource any more.
	deletePolicy := metav1.DeletePropagationBackground

	for _, resource := range resources {
		gvr := schema.GroupVersionResource{Group: resource.Group, Version: resource.Version, Resource: resource.Resource}
		u, err := dynamicClient.
			Resource(gvr).
			Namespace(resource.Namespace).
			Get(context.TODO(), resource.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			klog.V(2).Infof("Resource %v with key %s/%s is removed Successfully", gvr, resource.Namespace, resource.Name)
			continue
		}

		if err != nil {
			errs = append(errs, fmt.Errorf(
				"Failed to get resource %v with key %s/%s: %w",
				gvr, resource.Namespace, resource.Name, err))
			continue
		}

		existingOwner := u.GetOwnerReferences()

		// If it is not owned by us, skip
		if !IsOwnedBy(owner, existingOwner) {
			continue
		}

		// Merge with the existing owners to move the owner.
		modified := resourcemerge.BoolPtr(false)
		resourcemerge.MergeOwnerRefs(modified, &existingOwner, []metav1.OwnerReference{*ownerCopy})

		// If there are still any other existing owners (not only ManifestWorks), update ownerrefs only.
		if len(existingOwner) > 0 {
			if !*modified {
				continue
			}

			u.SetOwnerReferences(existingOwner)
			_, err = dynamicClient.Resource(gvr).Namespace(resource.Namespace).Update(context.TODO(), u, metav1.UpdateOptions{})
			if err != nil {
				errs = append(errs, fmt.Errorf(
					"Failed to remove owner from resource %v with key %s/%s: %w",
					gvr, resource.Namespace, resource.Name, err))
			}

			continue
		}

		if resource.UID != string(u.GetUID()) {
			// the traced instance has been deleted, and forget this item.
			continue
		}

		if u.GetDeletionTimestamp() != nil && !u.GetDeletionTimestamp().IsZero() {
			resourcesPendingFinalization = append(resourcesPendingFinalization, resource)
			continue
		}

		// delete the resource which is not deleted yet
		uid := types.UID(resource.UID)
		err = dynamicClient.
			Resource(gvr).
			Namespace(resource.Namespace).
			Delete(context.TODO(), resource.Name, metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{
					UID: &uid,
				},
				PropagationPolicy: &deletePolicy,
			})
		if errors.IsNotFound(err) {
			continue
		}
		// forget this item if the UID precondition check fails
		if errors.IsConflict(err) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"Failed to delete resource %v with key %s/%s: %w",
				gvr, resource.Namespace, resource.Name, err))
			continue
		}

		resourcesPendingFinalization = append(resourcesPendingFinalization, resource)
		recorder.Eventf("ResourceDeleted", "Deleted resource %v with key %s/%s because %s.", gvr, resource.Namespace, resource.Name, reason)
	}

	return resourcesPendingFinalization, errs
}

// GuessObjectGroupVersionKind returns GVK for the passed runtime object.
func GuessObjectGroupVersionKind(object runtime.Object) (*schema.GroupVersionKind, error) {
	gvk := resourcehelper.GuessObjectGroupVersionKind(object)
	// return gvk if found
	if gvk.Kind != unknownKind {
		return &gvk, nil
	}

	// otherwise fall back to genericScheme
	if kinds, _, _ := genericScheme.ObjectKinds(object); len(kinds) > 0 {
		return &kinds[0], nil
	}

	return nil, fmt.Errorf("cannot get gvk of %v", object)
}

// RemoveFinalizer removes a finalizer from the list.  It mutates its input.
func RemoveFinalizer(object runtime.Object, finalizerName string) {
	accessor, _ := meta.Accessor(object)
	finalizers := accessor.GetFinalizers()
	newFinalizers := []string{}
	for i := range finalizers {
		if finalizers[i] == finalizerName {
			continue
		}
		newFinalizers = append(newFinalizers, finalizers[i])
	}
	accessor.SetFinalizers(newFinalizers)
}

// AppliedManifestworkQueueKeyFunc return manifestwork key from appliedmanifestwork
func AppliedManifestworkQueueKeyFunc(hubhash string) factory.ObjectQueueKeyFunc {
	return func(obj runtime.Object) string {
		accessor, _ := meta.Accessor(obj)
		if !strings.HasPrefix(accessor.GetName(), hubhash) {
			return ""
		}

		return strings.TrimPrefix(accessor.GetName(), hubhash+"-")
	}
}

// HubHash returns a hash of hubserver
// NOTE: the length of hash string is 64, meaning the length of manifestwork name should be less than 189
func HubHash(hubServer string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(hubServer)))
}

// IsOwnedBy check if owner exists in the ownerrefs.
func IsOwnedBy(myOwner metav1.OwnerReference, existingOwners []metav1.OwnerReference) bool {
	for _, owner := range existingOwners {
		if myOwner.UID == owner.UID {
			return true
		}
	}
	return false
}

func NewAppliedManifestWorkOwner(appliedWork *workapiv1.AppliedManifestWork) *metav1.OwnerReference {
	return &metav1.OwnerReference{
		APIVersion: workapiv1.GroupVersion.WithKind("AppliedManifestWork").GroupVersion().String(),
		Kind:       workapiv1.GroupVersion.WithKind("AppliedManifestWork").Kind,
		Name:       appliedWork.Name,
		UID:        appliedWork.UID,
	}
}
