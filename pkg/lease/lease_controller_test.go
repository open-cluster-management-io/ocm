package lease

import (
	"context"
	"net/http"
	"testing"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
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
	lease := kubeClient.Actions()[1].(clienttesting.CreateActionImpl).Object.(*coordinationv1.Lease)
	if lease.ObjectMeta.Namespace != agentNs {
		t.Errorf(
			"The namespace of lease is not correct, expected %s, actual %s",
			lease.ObjectMeta.Namespace, agentNs)
	}

	//update lease
	kubeClient.ClearActions()
	leaseReconciler.reconcile(context.TODO())
	addontesting.AssertActions(t, kubeClient.Actions(), "get", "update")
}

func TestReconcileWithInvalidLease(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	hubClient := kubefake.NewSimpleClientset()

	leaseReconciler := &leaseUpdater{
		kubeClient:           kubeClient,
		hubKubeClient:        hubClient,
		leaseName:            leaseName,
		clusterName:          "cluster1",
		leaseDurationSeconds: 1,
		leaseNamespace:       agentNs,
	}

	// Add a reactor on fake client to throw error when get lease
	kubeClient.PrependReactor("create", "leases", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetVerb() != "create" {
			return false, nil, nil
		}

		return true, &coordinationv1.Lease{}, &errors.StatusError{
			ErrStatus: metav1.Status{
				Status:  metav1.StatusFailure,
				Code:    http.StatusNotFound,
				Reason:  metav1.StatusReasonNotFound,
				Message: "Fake test error",
			},
		}
	})

	//create lease
	leaseReconciler.reconcile(context.TODO())
	addontesting.AssertActions(t, hubClient.Actions(), "get", "create")

	lease := hubClient.Actions()[1].(clienttesting.CreateActionImpl).Object.(*coordinationv1.Lease)
	if lease.ObjectMeta.Namespace != "cluster1" {
		t.Errorf(
			"The namespace of lease is not correct, expected %s, actual cluster1",
			lease.ObjectMeta.Namespace)
	}
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
