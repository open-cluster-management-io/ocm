package helper

import (
	"context"
	"fmt"
	"time"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// MergeManifestConditions return a new ManifestCondition array which merges the existing manifest
// conditions and the new manifest conditions. Rules to match ManifestCondition between two arrays:
// 1. match the manifest condition with the whole ManifestResourceMeta;
// 2. if not matched, try to match with properties in ManifestResourceMeta other than ordinal
// 3. otherwise, try to match with ordinal only in ManifestResourceMeta
func MergeManifestConditions(conditions, newConditions []workapiv1.ManifestCondition) []workapiv1.ManifestCondition {
	merged := []workapiv1.ManifestCondition{}

	// build search indices
	metaIndex := map[workapiv1.ManifestResourceMeta]workapiv1.ManifestCondition{}
	metaWithoutOridinalIndex := map[string]workapiv1.ManifestCondition{}
	ordinalIndex := map[int32]workapiv1.ManifestCondition{}

	duplicateKeys := []string{}
	for _, condition := range conditions {
		metaIndex[condition.ResourceMeta] = condition
		if key := manifestResourceMetaKey(condition.ResourceMeta); key != "" {
			if _, exists := metaWithoutOridinalIndex[key]; exists {
				duplicateKeys = append(duplicateKeys, key)
			} else {
				metaWithoutOridinalIndex[key] = condition
			}
		}
		ordinalIndex[condition.ResourceMeta.Ordinal] = condition
	}

	// remove key from index if it is not unique
	for _, key := range duplicateKeys {
		delete(metaWithoutOridinalIndex, key)
	}

	// try to match and merge manifest conditions
	for _, newCondition := range newConditions {
		// match with ResourceMeta
		condition, ok := metaIndex[newCondition.ResourceMeta]

		// match with properties in ResourceMeta other than ordinal if not found yet
		if !ok {
			condition, ok = metaWithoutOridinalIndex[manifestResourceMetaKey(newCondition.ResourceMeta)]
		}

		// match with ordinal in ResourceMeta if not found yet
		if !ok {
			condition, ok = ordinalIndex[newCondition.ResourceMeta.Ordinal]
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

func manifestResourceMetaKey(meta workapiv1.ManifestResourceMeta) string {
	key := fmt.Sprintf("%s:%s:%s:%s:%s:%s", meta.Group, meta.Version, meta.Kind, meta.Resource, meta.Namespace, meta.Name)
	if len(key) == 5 {
		return ""
	}

	return key
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
