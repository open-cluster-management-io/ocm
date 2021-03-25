package lease

import (
	"context"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
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
}

func NewLeaseUpdater(
	kubeClient kubernetes.Interface,
	leaseName, leaseNamespace string,
) LeaseUpdater {
	return &leaseUpdater{
		kubeClient:           kubeClient,
		leaseName:            leaseName,
		leaseNamespace:       leaseNamespace,
		leaseDurationSeconds: defaultLeaseDurationSeconds,
	}
}

func (r *leaseUpdater) Start(ctx context.Context) {
	wait.JitterUntilWithContext(context.TODO(), r.reconcile, time.Duration(r.leaseDurationSeconds)*time.Second, leaseUpdateJitterFactor, true)
}

func (r *leaseUpdater) reconcile(ctx context.Context) {
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
