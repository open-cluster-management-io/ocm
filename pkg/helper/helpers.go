package helper

import (
	"context"
	"time"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// FindManifestConditionByIndex finds manifest condition by index in manifests
func FindManifestConditionByIndex(index int32, conds []workapiv1.ManifestCondition) *workapiv1.ManifestCondition {
	// Finds the cond conds that ordinal is the same as index
	if conds == nil {
		return nil
	}
	for i, cond := range conds {
		if index == cond.ResourceMeta.Ordinal {
			return &conds[i]
		}
	}

	return nil
}

func SetManifestCondition(conds *[]workapiv1.ManifestCondition, cond workapiv1.ManifestCondition) {
	if conds == nil {
		conds = &[]workapiv1.ManifestCondition{}
	}

	existingCond := FindManifestConditionByIndex(cond.ResourceMeta.Ordinal, *conds)
	if existingCond == nil {
		*conds = append(*conds, cond)
		return
	}

	existingCond.ResourceMeta = cond.ResourceMeta
	existingCond.Conditions = cond.Conditions
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
		Type:    condition.Type,
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
