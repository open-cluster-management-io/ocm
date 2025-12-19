// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	gv := schema.GroupVersion{Group: addonv1alpha1.GroupName, Version: addonv1alpha1.GroupVersion.Version}
	scheme.AddKnownTypes(gv,
		&ManagedClusterAddOn{},
		&ClusterManagementAddOn{},
	)
	metav1.AddToGroupVersion(scheme, gv)
	return nil
}

// ManagedClusterAddOn wraps the v1alpha1 API type for conversion webhook
type ManagedClusterAddOn struct {
	addonv1alpha1.ManagedClusterAddOn
}

// DeepCopyObject returns a deep copy of the ManagedClusterAddOn wrapper
func (in *ManagedClusterAddOn) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy returns a deep copy of the ManagedClusterAddOn wrapper
func (in *ManagedClusterAddOn) DeepCopy() *ManagedClusterAddOn {
	if in == nil {
		return nil
	}
	out := new(ManagedClusterAddOn)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto deep copies the receiver into out
func (in *ManagedClusterAddOn) DeepCopyInto(out *ManagedClusterAddOn) {
	in.ManagedClusterAddOn.DeepCopyInto(&out.ManagedClusterAddOn)
}

// ClusterManagementAddOn wraps the v1alpha1 API type for conversion webhook
type ClusterManagementAddOn struct {
	addonv1alpha1.ClusterManagementAddOn
}

// DeepCopyObject returns a deep copy of the ClusterManagementAddOn wrapper
func (in *ClusterManagementAddOn) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy returns a deep copy of the ClusterManagementAddOn wrapper
func (in *ClusterManagementAddOn) DeepCopy() *ClusterManagementAddOn {
	if in == nil {
		return nil
	}
	out := new(ClusterManagementAddOn)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto deep copies the receiver into out
func (in *ClusterManagementAddOn) DeepCopyInto(out *ClusterManagementAddOn) {
	in.ClusterManagementAddOn.DeepCopyInto(&out.ClusterManagementAddOn)
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
