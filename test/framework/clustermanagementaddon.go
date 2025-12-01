package framework

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

// CreateClusterManagementAddOnV1Alpha1 creates a ClusterManagementAddOn using v1alpha1 API
func (hub *Hub) CreateClusterManagementAddOnV1Alpha1(name string, addon *addonv1alpha1.ClusterManagementAddOn) (*addonv1alpha1.ClusterManagementAddOn, error) {
	if addon == nil {
		addon = &addonv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: addonv1alpha1.ClusterManagementAddOnSpec{},
		}
	}
	return hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(
		context.TODO(), addon, metav1.CreateOptions{})
}

// GetClusterManagementAddOnV1Alpha1 gets a ClusterManagementAddOn using v1alpha1 API
func (hub *Hub) GetClusterManagementAddOnV1Alpha1(name string) (*addonv1alpha1.ClusterManagementAddOn, error) {
	return hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(
		context.TODO(), name, metav1.GetOptions{})
}

// UpdateClusterManagementAddOnV1Alpha1 updates a ClusterManagementAddOn using v1alpha1 API
func (hub *Hub) UpdateClusterManagementAddOnV1Alpha1(addon *addonv1alpha1.ClusterManagementAddOn) (*addonv1alpha1.ClusterManagementAddOn, error) {
	return hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().Update(
		context.TODO(), addon, metav1.UpdateOptions{})
}

// DeleteClusterManagementAddOnV1Alpha1 deletes a ClusterManagementAddOn using v1alpha1 API
func (hub *Hub) DeleteClusterManagementAddOnV1Alpha1(name string) error {
	return hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(
		context.TODO(), name, metav1.DeleteOptions{})
}

// CreateClusterManagementAddOnV1Beta1 creates a ClusterManagementAddOn using v1beta1 API
func (hub *Hub) CreateClusterManagementAddOnV1Beta1(name string, addon *addonv1beta1.ClusterManagementAddOn) (*addonv1beta1.ClusterManagementAddOn, error) {
	if addon == nil {
		addon = &addonv1beta1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: addonv1beta1.ClusterManagementAddOnSpec{},
		}
	}
	return hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().Create(
		context.TODO(), addon, metav1.CreateOptions{})
}

// GetClusterManagementAddOnV1Beta1 gets a ClusterManagementAddOn using v1beta1 API
func (hub *Hub) GetClusterManagementAddOnV1Beta1(name string) (*addonv1beta1.ClusterManagementAddOn, error) {
	return hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().Get(
		context.TODO(), name, metav1.GetOptions{})
}

// UpdateClusterManagementAddOnV1Beta1 updates a ClusterManagementAddOn using v1beta1 API
func (hub *Hub) UpdateClusterManagementAddOnV1Beta1(addon *addonv1beta1.ClusterManagementAddOn) (*addonv1beta1.ClusterManagementAddOn, error) {
	return hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().Update(
		context.TODO(), addon, metav1.UpdateOptions{})
}

// DeleteClusterManagementAddOnV1Beta1 deletes a ClusterManagementAddOn using v1beta1 API
func (hub *Hub) DeleteClusterManagementAddOnV1Beta1(name string) error {
	return hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(
		context.TODO(), name, metav1.DeleteOptions{})
}
