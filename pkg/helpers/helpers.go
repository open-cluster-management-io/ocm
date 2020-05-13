package helpers

import (
	"context"
	"fmt"
	"time"

	nucleusv1client "github.com/open-cluster-management/api/client/nucleus/clientset/versioned/typed/nucleus/v1"
	nucleusapiv1 "github.com/open-cluster-management/api/nucleus/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
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

type UpdateNucleusSpokeStatusFunc func(status *nucleusapiv1.SpokeCoreStatus) error

func UpdateNucleusSpokeStatus(
	ctx context.Context,
	client nucleusv1client.SpokeCoreInterface,
	nucleusSpokeCoreName string,
	updateFuncs ...UpdateNucleusSpokeStatusFunc) (*nucleusapiv1.SpokeCoreStatus, bool, error) {
	updated := false
	var updatedSpokeClusterStatus *nucleusapiv1.SpokeCoreStatus
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		spokeCore, err := client.Get(ctx, nucleusSpokeCoreName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &spokeCore.Status

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

		spokeCore.Status = *newStatus
		updatedSpokeCluster, err := client.UpdateStatus(ctx, spokeCore, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		updatedSpokeClusterStatus = &updatedSpokeCluster.Status
		updated = err == nil
		return err
	})

	return updatedSpokeClusterStatus, updated, err
}

func UpdateNucleusSpokeConditionFn(conds ...nucleusapiv1.StatusCondition) UpdateNucleusSpokeStatusFunc {
	return func(oldStatus *nucleusapiv1.SpokeCoreStatus) error {
		for _, cond := range conds {
			SetNucleusCondition(&oldStatus.Conditions, cond)
		}
		return nil
	}
}

func CleanUpStaticObject(
	ctx context.Context,
	client kubernetes.Interface,
	apiextensionclient apiextensionsclient.Interface,
	object runtime.Object) error {
	var err error
	switch t := object.(type) {
	case *corev1.Namespace:
		err = client.CoreV1().Namespaces().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.Service:
		err = client.CoreV1().Services(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.ServiceAccount:
		err = client.CoreV1().ServiceAccounts(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.ConfigMap:
		err = client.CoreV1().ConfigMaps(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.Secret:
		err = client.CoreV1().Secrets(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.ClusterRole:
		err = client.RbacV1().ClusterRoles().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.ClusterRoleBinding:
		err = client.RbacV1().ClusterRoleBindings().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.Role:
		err = client.RbacV1().Roles(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.RoleBinding:
		err = client.RbacV1().RoleBindings(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *apiextensionsv1.CustomResourceDefinition:
		err = apiextensionclient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *apiextensionsv1beta1.CustomResourceDefinition:
		err = apiextensionclient.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(ctx, t.Name, metav1.DeleteOptions{})
	default:
		err = fmt.Errorf("unhandled type %T", object)
	}
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}
