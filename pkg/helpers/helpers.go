package helpers

import (
	"context"
	"net/url"
	"time"

	spokeclusterclientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	spokeclusterv1 "github.com/open-cluster-management/api/cluster/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func IsConditionTrue(condition *spokeclusterv1.StatusCondition) bool {
	if condition == nil {
		return false
	}
	return condition.Status == metav1.ConditionTrue
}

func FindSpokeClusterCondition(conditions []spokeclusterv1.StatusCondition, conditionType string) *spokeclusterv1.StatusCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func SetSpokeClusterCondition(conditions *[]spokeclusterv1.StatusCondition, newCondition spokeclusterv1.StatusCondition) {
	if conditions == nil {
		conditions = &[]spokeclusterv1.StatusCondition{}
	}
	existingCondition := FindSpokeClusterCondition(*conditions, newCondition.Type)
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

type UpdateSpokeClusterStatusFunc func(status *spokeclusterv1.SpokeClusterStatus) error

func UpdateSpokeClusterStatus(
	ctx context.Context,
	client spokeclusterclientset.Interface,
	spokeClusterName string,
	updateFuncs ...UpdateSpokeClusterStatusFunc) (*spokeclusterv1.SpokeClusterStatus, bool, error) {
	updated := false
	var updatedSpokeClusterStatus *spokeclusterv1.SpokeClusterStatus
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		spokeCluster, err := client.ClusterV1().SpokeClusters().Get(ctx, spokeClusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &spokeCluster.Status

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

		spokeCluster.Status = *newStatus
		updatedSpokeCluster, err := client.ClusterV1().SpokeClusters().UpdateStatus(ctx, spokeCluster, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		updatedSpokeClusterStatus = &updatedSpokeCluster.Status
		updated = err == nil
		return err
	})

	return updatedSpokeClusterStatus, updated, err
}

func UpdateSpokeClusterConditionFn(cond spokeclusterv1.StatusCondition) UpdateSpokeClusterStatusFunc {
	return func(oldStatus *spokeclusterv1.SpokeClusterStatus) error {
		SetSpokeClusterCondition(&oldStatus.Conditions, cond)
		return nil
	}
}

// Check whether a CSR is in terminal state
func IsCSRInTerminalState(status *certificatesv1beta1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1beta1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1beta1.CertificateDenied {
			return true
		}
	}
	return false
}

// IsValidHTTPSURL validate whether a URL is https URL
func IsValidHTTPSURL(serverURL string) bool {
	if serverURL == "" {
		return false
	}

	parsedServerURL, err := url.Parse(serverURL)
	if err != nil {
		return false
	}

	if parsedServerURL.Scheme != "https" {
		return false
	}

	return true
}
