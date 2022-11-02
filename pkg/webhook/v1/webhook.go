package v1

import (
	"k8s.io/client-go/kubernetes"
	v1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ManagedClusterWebhook struct {
	kubeClient kubernetes.Interface
}

func (r *ManagedClusterWebhook) Init(mgr ctrl.Manager) error {
	err := r.SetupWebhookWithManager(mgr)
	if err != nil {
		return err
	}
	r.kubeClient, err = kubernetes.NewForConfig(mgr.GetConfig())
	return err
}

func (r *ManagedClusterWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(r).
		WithDefaulter(r).
		For(&v1.ManagedCluster{}).
		Complete()
}
