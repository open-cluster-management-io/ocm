package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/api/cluster/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	schemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// Install is a function which adds this version to a scheme
	Install = schemeBuilder.AddToScheme
)

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(v1beta1.GroupVersion,
		&ManagedClusterSet{},
		&v1beta1.ManagedClusterSetBinding{},
	)
	metav1.AddToGroupVersion(scheme, v1beta1.GroupVersion)
	return nil
}

type ManagedClusterSet struct {
	v1beta1.ManagedClusterSet
}

type ManagedClusterSetBindingWebhook struct {
	kubeClient kubernetes.Interface
}

func (r *ManagedClusterSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

func (b *ManagedClusterSetBindingWebhook) Init(mgr ctrl.Manager) error {
	err := b.SetupWebhookWithManager(mgr)
	if err != nil {
		return err
	}
	b.kubeClient, err = kubernetes.NewForConfig(mgr.GetConfig())
	return err
}

// SetExternalKubeClientSet is function to enable the webhook injecting to kube admssion
func (b *ManagedClusterSetBindingWebhook) SetExternalKubeClientSet(client kubernetes.Interface) {
	b.kubeClient = client
}

func (b *ManagedClusterSetBindingWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(b).
		For(&v1beta1.ManagedClusterSetBinding{}).
		Complete()
}

func ClustersetbindingGroupKind() schema.GroupKind {
	return schema.GroupKind{
		Group: v1beta1.GroupName,
		Kind:  "ManagedClusterSetBinding",
	}
}
