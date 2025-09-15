package framework

import (
	"context"
	"fmt"
	"time"

	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

func (hub *Hub) CreateManagedClusterAddOn(managedClusterNamespace, addOnName, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterNamespace).Create(
		context.TODO(),
		&addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: managedClusterNamespace,
				Name:      addOnName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{},
		},
		metav1.CreateOptions{},
	)

	if err != nil {
		return err
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		addOn, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterNamespace).Get(
			context.TODO(), addOnName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if addOn.Status.Namespace == installNamespace {
			return nil
		}
		addOn.Status.Namespace = installNamespace
		_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterNamespace).UpdateStatus(
			context.TODO(), addOn, metav1.UpdateOptions{})
		return err
	})
}

func (hub *Hub) CreateManagedClusterAddOnLease(addOnInstallNamespace, addOnName string) error {
	if _, err := hub.KubeClient.CoreV1().Namespaces().Create(
		context.TODO(),
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnInstallNamespace,
			},
		},
		metav1.CreateOptions{},
	); err != nil {
		return err
	}

	_, err := hub.KubeClient.CoordinationV1().Leases(addOnInstallNamespace).Create(
		context.TODO(),
		&coordv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: addOnInstallNamespace,
			},
			Spec: coordv1.LeaseSpec{
				RenewTime: &metav1.MicroTime{Time: time.Now()},
			},
		},
		metav1.CreateOptions{},
	)
	return err
}

func (hub *Hub) CheckManagedClusterAddOnStatus(managedClusterNamespace, addOnName string) error {
	addOn, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterNamespace).Get(context.TODO(), addOnName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if addOn.Status.Conditions == nil {
		return fmt.Errorf("there is no conditions in addon %v/%v", managedClusterNamespace, addOnName)
	}

	if !meta.IsStatusConditionTrue(addOn.Status.Conditions, "Available") {
		return fmt.Errorf("the addon %v/%v available condition is not true, %v",
			managedClusterNamespace, addOnName, addOn.Status.Conditions)
	}

	return nil
}
