package v1

import (
	"k8s.io/client-go/kubernetes"
	v1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ManifestWorkWebhook struct {
	kubeClient kubernetes.Interface
}

func (r *ManifestWorkWebhook) Init(mgr ctrl.Manager) error {
	err := r.SetupWebhookWithManager(mgr)
	if err != nil {
		return err
	}
	r.kubeClient, err = kubernetes.NewForConfig(mgr.GetConfig())
	return err
}

// SetExternalKubeClientSet is function to enable the webhook injecting to kube admission
func (r *ManifestWorkWebhook) SetExternalKubeClientSet(client kubernetes.Interface) {
	r.kubeClient = client
}

func (r *ManifestWorkWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(r).
		For(&v1.ManifestWork{}).
		Complete()
}
