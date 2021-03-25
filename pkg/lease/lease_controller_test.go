package lease

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	leaseName = "lease"
	agentNs   = "open-cluster-management-agent"
)

func TestLeaseReconciler_Reconcile(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Namespace{})
	kubeClient := kubefake.NewSimpleClientset()

	leaseReconciler := &leaseUpdater{
		kubeClient:           kubeClient,
		leaseName:            leaseName,
		leaseDurationSeconds: 1,
		leaseNamespace:       agentNs,
	}

	//create lease
	leaseReconciler.reconcile(context.TODO())
	if !actionExist(kubeClient, "create") {
		t.Errorf("failed to create lease")
	}

	//update lease
	leaseReconciler.reconcile(context.TODO())
	if !actionExist(kubeClient, "update") {
		t.Errorf("failed to update lease")
	}
}

func actionExist(kubeClient *kubefake.Clientset, existAction string) bool {
	for _, action := range kubeClient.Actions() {
		if action.GetVerb() == existAction {
			return true
		}
	}
	return false
}
