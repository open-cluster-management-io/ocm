package helpers

import (
	"context"
	"time"

	nucleusv1client "github.com/open-cluster-management/api/client/nucleus/clientset/versioned/typed/nucleus/v1"
	nucleusapiv1 "github.com/open-cluster-management/api/nucleus/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func IsConditionTrue(condition *nucleusapiv1.StatusCondition) bool {
	if condition == nil {
		return false
	}
	return condition.Status == metav1.ConditionTrue
}

func FindNucleusCondition(conditions []nucleusapiv1.StatusCondition, conditionType string) *nucleusapiv1.StatusCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func SetNucleusCondition(conditions *[]nucleusapiv1.StatusCondition, newCondition nucleusapiv1.StatusCondition) {
	if conditions == nil {
		conditions = &[]nucleusapiv1.StatusCondition{}
	}
	existingCondition := FindNucleusCondition(*conditions, newCondition.Type)
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

type UpdateNucleusHubStatusFunc func(status *nucleusapiv1.HubCoreStatus) error

func UpdateNucleusHubStatus(
	ctx context.Context,
	client nucleusv1client.HubCoreInterface,
	nucleusHubCoreName string,
	updateFuncs ...UpdateNucleusHubStatusFunc) (*nucleusapiv1.HubCoreStatus, bool, error) {
	updated := false
	var updatedSpokeClusterStatus *nucleusapiv1.HubCoreStatus
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		hubCore, err := client.Get(ctx, nucleusHubCoreName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &hubCore.Status

		newStatus := oldStatus.DeepCopy()
		for _, update := range updateFuncs {
			if err := update(newStatus); err != nil {
				return err
			}
		}
		if equality.Semantic.DeepEqual(oldStatus, newStatus) {
			// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
			updatedSpokeClusterStatus = newStatus
			return nil
		}

		hubCore.Status = *newStatus
		updatedSpokeCluster, err := client.UpdateStatus(ctx, hubCore, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		updatedSpokeClusterStatus = &updatedSpokeCluster.Status
		updated = err == nil
		return err
	})

	return updatedSpokeClusterStatus, updated, err
}

func UpdateNucleusHubConditionFn(conds ...nucleusapiv1.StatusCondition) UpdateNucleusHubStatusFunc {
	return func(oldStatus *nucleusapiv1.HubCoreStatus) error {
		for _, cond := range conds {
			SetNucleusCondition(&oldStatus.Conditions, cond)
		}
		return nil
	}
}
