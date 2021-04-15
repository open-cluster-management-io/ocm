package lease

import (
	"context"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
)

const (
	leaseUpdateJitterFactor     = 0.25
	defaultLeaseDurationSeconds = 60
)

// LeaseUpdater is to update lease with certain period
type LeaseUpdater interface {
	// Start starts a goroutine to update lease
	Start(ctx context.Context)
}

// leaseUpdater update lease of with given name and namespace
type leaseUpdater struct {
	kubeClient           kubernetes.Interface
	leaseName            string
	leaseNamespace       string
	leaseDurationSeconds int32
	healthCheckFuncs     []func() bool
}

func NewLeaseUpdater(
	kubeClient kubernetes.Interface,
	leaseName, leaseNamespace string,
	healthCheckFuncs ...func() bool,
) LeaseUpdater {
	return &leaseUpdater{
		kubeClient:           kubeClient,
		leaseName:            leaseName,
		leaseNamespace:       leaseNamespace,
		leaseDurationSeconds: defaultLeaseDurationSeconds,
		healthCheckFuncs:     healthCheckFuncs,
	}
}

func (r *leaseUpdater) Start(ctx context.Context) {
	wait.JitterUntilWithContext(context.TODO(), r.reconcile, time.Duration(r.leaseDurationSeconds)*time.Second, leaseUpdateJitterFactor, true)
}

func (r *leaseUpdater) reconcile(ctx context.Context) {
	for _, f := range r.healthCheckFuncs {
		if !f() {
			// IF a healthy check fails, do not update lease.
			return
		}
	}
	lease, err := r.kubeClient.CoordinationV1().Leases(r.leaseNamespace).Get(context.TODO(), r.leaseName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		//create lease
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.leaseName,
				Namespace: r.leaseNamespace,
			},
			Spec: coordinationv1.LeaseSpec{
				LeaseDurationSeconds: &r.leaseDurationSeconds,
				RenewTime: &metav1.MicroTime{
					Time: time.Now(),
				},
			},
		}
		if _, err := r.kubeClient.CoordinationV1().Leases(r.leaseNamespace).Create(context.TODO(), lease, metav1.CreateOptions{}); err != nil {
			klog.Errorf("unable to create addon lease %q/%q on hub cluster. error:%v", r.leaseNamespace, r.leaseName, err)
		}
		return
	case err != nil:
		klog.Errorf("unable to get addon lease %q/%q on hub cluster. error:%v", r.leaseNamespace, r.leaseName, err)
		return
	default:
		//update lease
		lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}
		if _, err = r.kubeClient.CoordinationV1().Leases(r.leaseNamespace).Update(context.TODO(), lease, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("unable to update cluster lease %q/%q on hub cluster. error:%v", r.leaseNamespace, r.leaseName, err)
		}
		return
	}
}

// CheckAddonPodFunc checks whether the agent pod is running
func CheckAddonPodFunc(podGetter corev1client.PodsGetter, namespace, labelSelector string) func() bool {
	return func() bool {
		pods, err := podGetter.Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			klog.Errorf("Failed to get pods in namespace %s with label selector %s: %v", namespace, labelSelector, err)
			return false
		}

		// If one of the pods is running, we think the agent is serving.
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				return true
			}
		}

		return false
	}

}
