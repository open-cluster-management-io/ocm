package csr

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificateslisters "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/klog/v2"
	"open-cluster-management.io/registration/pkg/helpers"
)

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController struct {
	csrLister   certificateslisters.CertificateSigningRequestLister
	reconcilers []Reconciler
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController(
	csrInformer certificatesinformers.CertificateSigningRequestInformer,
	reconcilers []Reconciler,
	recorder events.Recorder) factory.Controller {
	c := &csrApprovingController{
		csrLister:   csrInformer.Lister(),
		reconcilers: reconcilers,
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, csrInformer.Informer()).
		WithSync(c.sync).
		ToController("CSRApprovingController", recorder)
}

func (c *csrApprovingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling CertificateSigningRequests %q", csrName)

	csr, err := c.csrLister.Get(csrName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	csr = csr.DeepCopy()
	// Current csr is in terminal state, do nothing.
	if helpers.IsCSRInTerminalState(&csr.Status) {
		return nil
	}

	csrInfo := newCSRInfo(csr)
	for _, r := range c.reconcilers {
		state, err := r.Reconcile(ctx, csrInfo, approveCSRV1Func(ctx, csr))
		if err != nil {
			return err
		}
		if state == reconcileStop {
			break
		}
	}

	return nil
}

func approveCSRV1Func(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) approveCSRFunc {
	return func(kubeClient kubernetes.Interface) error {
		// Auto approve the spoke cluster csr
		csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
			Type:    certificatesv1.CertificateApproved,
			Status:  corev1.ConditionTrue,
			Reason:  "AutoApprovedByHubCSRApprovingController",
			Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
		})
		_, err := kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{})
		return err
	}
}
