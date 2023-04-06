package csr

import (
	"context"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/registration/pkg/helpers"
)

type CSR interface {
	*certificatesv1.CertificateSigningRequest | *certificatesv1beta1.CertificateSigningRequest
}

type CSRLister[T CSR] interface {
	Get(name string) (T, error)
}

type CSRApprover[T CSR] interface {
	approve(ctx context.Context, csr T) approveCSRFunc
	isInTerminalState(csr T) bool
}

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController[T CSR] struct {
	lister      CSRLister[T]
	approver    CSRApprover[T]
	reconcilers []Reconciler
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController[T CSR](
	csrInformer cache.SharedIndexInformer,
	lister CSRLister[T],
	approver CSRApprover[T],
	reconcilers []Reconciler,
	recorder events.Recorder) factory.Controller {
	c := &csrApprovingController[T]{
		lister:      lister,
		approver:    approver,
		reconcilers: reconcilers,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, csrInformer).
		WithSync(c.sync).
		ToController("CSRApprovingController", recorder)
}

func (c *csrApprovingController[T]) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling CertificateSigningRequests %q", csrName)

	csr, err := c.lister.Get(csrName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if c.approver.isInTerminalState(csr) {
		return nil
	}

	csrInfo := newCSRInfo(csr)
	for _, r := range c.reconcilers {
		state, err := r.Reconcile(ctx, csrInfo, c.approver.approve(ctx, csr))
		if err != nil {
			return err
		}
		if state == reconcileStop {
			break
		}
	}

	return nil
}

// CSRV1Approver implement CSRApprover interface
type CSRV1Approver struct {
	kubeClient kubernetes.Interface
}

func NewCSRV1Approver(client kubernetes.Interface) *CSRV1Approver {
	return &CSRV1Approver{kubeClient: client}
}

func (c *CSRV1Approver) isInTerminalState(csr *certificatesv1.CertificateSigningRequest) bool {
	return helpers.IsCSRInTerminalState(&csr.Status)
}

func (c *CSRV1Approver) approve(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) approveCSRFunc {
	return func(kubeClient kubernetes.Interface) error {
		csrCopy := csr.DeepCopy()
		// Auto approve the spoke cluster csr
		csrCopy.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
			Type:    certificatesv1.CertificateApproved,
			Status:  corev1.ConditionTrue,
			Reason:  "AutoApprovedByHubCSRApprovingController",
			Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
		})
		_, err := kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csrCopy.Name, csrCopy, metav1.UpdateOptions{})
		return err
	}
}

type CSRV1beta1Approver struct {
	kubeClient kubernetes.Interface
}

func NewCSRV1beta1Approver(client kubernetes.Interface) *CSRV1beta1Approver {
	return &CSRV1beta1Approver{kubeClient: client}
}

func (c *CSRV1beta1Approver) isInTerminalState(csr *certificatesv1beta1.CertificateSigningRequest) bool {
	return helpers.Isv1beta1CSRInTerminalState(&csr.Status)
}

func (c *CSRV1beta1Approver) approve(ctx context.Context, csr *certificatesv1beta1.CertificateSigningRequest) approveCSRFunc {
	return func(kubeClient kubernetes.Interface) error {
		csrCopy := csr.DeepCopy()
		// Auto approve the spoke cluster csr
		csrCopy.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
			Type:    certificatesv1beta1.CertificateApproved,
			Status:  corev1.ConditionTrue,
			Reason:  "AutoApprovedByHubCSRApprovingController",
			Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
		})
		_, err := kubeClient.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(ctx, csrCopy, metav1.UpdateOptions{})
		return err
	}
}
