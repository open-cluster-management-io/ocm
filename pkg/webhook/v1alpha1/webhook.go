package v1alpha1

import (
	"k8s.io/client-go/kubernetes"
	v1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type PlaceManifestWorkWebhook struct {
	kubeClient kubernetes.Interface
}

func (r *PlaceManifestWorkWebhook) Init(mgr ctrl.Manager) error {
	err := r.SetupWebhookWithManager(mgr)
	if err != nil {
		return err
	}
	r.kubeClient, err = kubernetes.NewForConfig(mgr.GetConfig())
	return err
}

// SetExternalKubeClientSet is function to enable the webhook injecting to kube admssion
func (r *PlaceManifestWorkWebhook) SetExternalKubeClientSet(client kubernetes.Interface) {
	r.kubeClient = client
}

func (r *PlaceManifestWorkWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(r).
		For(&v1alpha1.PlaceManifestWork{}).
		Complete()
}
