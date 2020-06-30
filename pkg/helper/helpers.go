package helper

import (
	"context"
	"fmt"
	"time"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
)

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
		Conditions:   MergeStatusConditions(condition.Conditions, newCondition.Conditions),
	}
}

// MergeStatusConditions returns a new status condition array with merged status conditions. It is based on newConditions,
// and merges the corresponding existing conditions if exists.
func MergeStatusConditions(conditions []workapiv1.StatusCondition, newConditions []workapiv1.StatusCondition) []workapiv1.StatusCondition {
	merged := []workapiv1.StatusCondition{}

	cm := map[string]workapiv1.StatusCondition{}
	for _, condition := range conditions {
		cm[condition.Type] = condition
	}

	for _, newCondition := range newConditions {
		// merge two conditions if necessary
		if condition, ok := cm[newCondition.Type]; ok {
			merged = append(merged, mergeStatusCondition(condition, newCondition))
			continue
		}

		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		merged = append(merged, newCondition)
	}

	return merged
}

// mergeStatusCondition returns a new status condition which merges the properties from the two input conditions.
// It assumes the two conditions have the same condition type.
func mergeStatusCondition(condition, newCondition workapiv1.StatusCondition) workapiv1.StatusCondition {
	merged := workapiv1.StatusCondition{
		Type:    newCondition.Type,
		Status:  newCondition.Status,
		Reason:  newCondition.Reason,
		Message: newCondition.Message,
	}

	if condition.Status == newCondition.Status {
		merged.LastTransitionTime = condition.LastTransitionTime
	} else {
		merged.LastTransitionTime = metav1.NewTime(time.Now())
	}

	return merged
}

// SetStatusCondition set a condition in conditons list
func SetStatusCondition(conditions *[]workapiv1.StatusCondition, newCondition workapiv1.StatusCondition) {
	if conditions == nil {
		conditions = &[]workapiv1.StatusCondition{}
	}
	existingCondition := FindOperatorCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		*conditions = append(*conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}

	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
}

// RemoveStatusCondition removes a certain conditions
func RemoveStatusCondition(conditions *[]workapiv1.StatusCondition, conditionType string) {
	if conditions == nil {
		conditions = &[]workapiv1.StatusCondition{}
	}
	newConditions := []workapiv1.StatusCondition{}
	for _, condition := range *conditions {
		if condition.Type != conditionType {
			newConditions = append(newConditions, condition)
		}
	}

	*conditions = newConditions
}

// FindOperatorCondition finds a condition in conditions list
func FindOperatorCondition(conditions []workapiv1.StatusCondition, conditionType string) *workapiv1.StatusCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

// IsStatusConditionTrue returns true if all conditions is in the status of true
func IsStatusConditionTrue(conditions []workapiv1.StatusCondition, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(conditions, conditionType, metav1.ConditionTrue)
}

// IsStatusConditionFalse returns false if all conditions in the status of false
func IsStatusConditionFalse(conditions []workapiv1.StatusCondition, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(conditions, conditionType, metav1.ConditionFalse)
}

// IsStatusConditionPresentAndEqual return if all conditions matches the status.
func IsStatusConditionPresentAndEqual(conditions []workapiv1.StatusCondition, conditionType string, status metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == status
		}
	}
	return false
}

type UpdateManifestWorkStatusFunc func(status *workapiv1.ManifestWorkStatus) error

func UpdateManifestWorkStatus(
	ctx context.Context,
	client workv1client.ManifestWorkInterface,
	workName string,
	updateFuncs ...UpdateManifestWorkStatusFunc) (*workapiv1.ManifestWorkStatus, bool, error) {
	updated := false
	var updatedWorkStatus *workapiv1.ManifestWorkStatus
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		manifestWork, err := client.Get(ctx, workName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &manifestWork.Status

		newStatus := oldStatus.DeepCopy()
		for _, update := range updateFuncs {
			if err := update(newStatus); err != nil {
				return err
			}
		}
		if equality.Semantic.DeepEqual(oldStatus, newStatus) {
			// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
			updatedWorkStatus = newStatus
			return nil
		}

		manifestWork.Status = *newStatus
		updatedManifestWork, err := client.UpdateStatus(ctx, manifestWork, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		updatedWorkStatus = &updatedManifestWork.Status
		updated = err == nil
		return err
	})

	return updatedWorkStatus, updated, err
}

// DeleteAppliedResources deletes all given applied resources and returns those pending for finalization
func DeleteAppliedResources(resources []workapiv1.AppliedManifestResourceMeta, dynamicClient dynamic.Interface) ([]workapiv1.AppliedManifestResourceMeta, []error) {
	var resourcesPendingFinalization []workapiv1.AppliedManifestResourceMeta
	var errs []error

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

		pendingFinalization := u.GetDeletionTimestamp() != nil && !u.GetDeletionTimestamp().IsZero()
		waitingForFinalization := len(resource.UID) > 0
		if pendingFinalization {
			if waitingForFinalization && resource.UID != string(u.GetUID()) {
				// the instance is deleted, so do not add to the resourcesPendingDeletion list
				continue
			}

			// if we aren't waiting for finalization or the UID does match, then we want to ensure we add it to the pending list
			newResource := copyAppliedResource(resource)
			newResource.UID = string(u.GetUID())
			resourcesPendingFinalization = append(resourcesPendingFinalization, newResource)
			continue
		}

		// forget this item if the UID does not match the resource UID we previously deleted
		if waitingForFinalization && resource.UID != string(u.GetUID()) {
			continue
		}

		// delete the resource which is not deleted yet
		uid := u.GetUID()
		err = dynamicClient.
			Resource(gvr).
			Namespace(resource.Namespace).
			Delete(context.TODO(), resource.Name, metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{
					UID: &uid,
				},
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

		// set UID of applied resource once deletion is successful
		newResource := copyAppliedResource(resource)
		newResource.UID = string(u.GetUID())
		resourcesPendingFinalization = append(resourcesPendingFinalization, newResource)
		klog.V(2).Infof("Delete resource %v with key %s/%s", gvr, resource.Namespace, resource.Name)
	}

	return resourcesPendingFinalization, errs
}

func copyAppliedResource(resource workapiv1.AppliedManifestResourceMeta) workapiv1.AppliedManifestResourceMeta {
	return workapiv1.AppliedManifestResourceMeta{
		Group:     resource.Group,
		Version:   resource.Version,
		Resource:  resource.Resource,
		Name:      resource.Name,
		Namespace: resource.Namespace,
		UID:       resource.UID,
	}
}
