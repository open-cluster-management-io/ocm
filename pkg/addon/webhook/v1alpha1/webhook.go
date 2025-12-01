// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

var (
	schemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// Install is a function which adds this version to a scheme
	Install = schemeBuilder.AddToScheme
)

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(addonv1alpha1.GroupVersion,
		&ManagedClusterAddOn{},
		&ClusterManagementAddOn{},
	)
	metav1.AddToGroupVersion(scheme, addonv1alpha1.GroupVersion)
	return nil
}

// ManagedClusterAddOn wraps the v1alpha1 API type for conversion webhook
type ManagedClusterAddOn struct {
	addonv1alpha1.ManagedClusterAddOn
}

// ClusterManagementAddOn wraps the v1alpha1 API type for conversion webhook
type ClusterManagementAddOn struct {
	addonv1alpha1.ClusterManagementAddOn
}

// ManagedClusterAddOnWebhook implements the webhook for ManagedClusterAddOn v1alpha1 (Hub version)
type ManagedClusterAddOnWebhook struct{}

func (w *ManagedClusterAddOnWebhook) Init(mgr ctrl.Manager) error {
	return (&ManagedClusterAddOn{}).SetupWebhookWithManager(mgr)
}

// SetupWebhookWithManager sets up the webhook with manager for ManagedClusterAddOn
func (r *ManagedClusterAddOn) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// ClusterManagementAddOnWebhook implements the webhook for ClusterManagementAddOn v1alpha1 (Hub version)
type ClusterManagementAddOnWebhook struct{}

func (w *ClusterManagementAddOnWebhook) Init(mgr ctrl.Manager) error {
	return (&ClusterManagementAddOn{}).SetupWebhookWithManager(mgr)
}

// SetupWebhookWithManager sets up the webhook with manager for ClusterManagementAddOn
func (r *ClusterManagementAddOn) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}
