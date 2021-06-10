package lease

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
)

const (
	leaseName = "lease"
	agentNs   = "open-cluster-management-agent"
)

func TestReconcile(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()

	leaseReconciler := &leaseUpdater{
		kubeClient:           kubeClient,
		leaseName:            leaseName,
		leaseDurationSeconds: 1,
		leaseNamespace:       agentNs,
	}

	//create lease
	leaseReconciler.reconcile(context.TODO())
	addontesting.AssertActions(t, kubeClient.Actions(), "get", "create")

	//update lease
	kubeClient.ClearActions()
	leaseReconciler.reconcile(context.TODO())
	addontesting.AssertActions(t, kubeClient.Actions(), "get", "update")
}

func TestReconcileWithHealthCheck(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()

	healthy := false
	leaseReconciler := &leaseUpdater{
		kubeClient:           kubeClient,
		leaseName:            leaseName,
		leaseDurationSeconds: 1,
		leaseNamespace:       agentNs,
		healthCheckFuncs: []func() bool{
			func() bool { return healthy },
		},
	}

	//create lease
	leaseReconciler.reconcile(context.TODO())
	addontesting.AssertNoActions(t, kubeClient.Actions())

	healthy = true
	leaseReconciler.reconcile(context.TODO())
	addontesting.AssertActions(t, kubeClient.Actions(), "get", "create")
}

func TestCheckAddonPodFunc(t *testing.T) {
	cases := []struct {
		name     string
		pods     []runtime.Object
		expected bool
	}{
		{
			name:     "no pod",
			pods:     []runtime.Object{},
			expected: false,
		},
		{
			name: "incorrect label",
			pods: []runtime.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "test"}},
			},
			expected: false,
		},
		{
			name: "no running state",
			pods: []runtime.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "test", Labels: map[string]string{"addon": "test"}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "test", Labels: map[string]string{"addon": "test"}}},
			},
			expected: false,
		},
		{
			name: "running state",
			pods: []runtime.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "test", Labels: map[string]string{"addon": "test"}}},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "test", Labels: map[string]string{"addon": "test"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.pods...)
			actual := CheckAddonPodFunc(kubeClient.CoreV1(), "test", "addon=test")()
			if c.expected != actual {
				t.Errorf("Failed to check pod expect %v, actual %v", c.expected, actual)
			}
		})
	}

}
