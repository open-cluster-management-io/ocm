package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"open-cluster-management.io/api/cluster/v1beta2"
)

var (
	schemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// Install is a function which adds this version to a scheme
	Install = schemeBuilder.AddToScheme
)

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	gv := schema.GroupVersion{Group: v1beta2.GroupName, Version: v1beta2.GroupVersion.Version}
	scheme.AddKnownTypes(gv,
		&ManagedClusterSet{},
		&v1beta2.ManagedClusterSetBinding{},
	)
	metav1.AddToGroupVersion(scheme, gv)
	return nil
}

type ManagedClusterSet struct {
	v1beta2.ManagedClusterSet
}

type ManagedClusterSetBindingWebhook struct {
	kubeClient kubernetes.Interface
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
		For(&v1beta2.ManagedClusterSetBinding{}).
		Complete()
}
