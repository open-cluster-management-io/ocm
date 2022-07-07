package csr

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	certificatesv1beta1informers "k8s.io/client-go/informers/certificates/v1beta1"
	certificatesv1beta1lister "k8s.io/client-go/listers/certificates/v1beta1"
)

// v1beta1CSRApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type v1beta1CSRApprovingController struct {
	kubeClient    kubernetes.Interface
	csrLister     certificatesv1beta1lister.CertificateSigningRequestLister
	eventRecorder events.Recorder
}

func NewV1beta1CSRApprovingController(
	kubeClient kubernetes.Interface,
	v1beta1CSRInformer certificatesv1beta1informers.CertificateSigningRequestInformer,
	recorder events.Recorder) factory.Controller {

	c := &v1beta1CSRApprovingController{
		kubeClient:    kubeClient,
		csrLister:     v1beta1CSRInformer.Lister(),
		eventRecorder: recorder.WithComponentSuffix("csr-approving-controller"),
	}

	return factory.New().WithInformersQueueKeyFunc(func(obj runtime.Object) string {
		accessor, _ := meta.Accessor(obj)
		return accessor.GetName()
	}, v1beta1CSRInformer.Informer()).
		WithSync(c.sync).
		ToController("V1Beta1CSRApprovingController", recorder)
}

func (c *v1beta1CSRApprovingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
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
	if helpers.Isv1beta1CSRInTerminalState(&csr.Status) {
		return nil
	}

	csrInfo := newCSRInfo(csr)
	// Check whether current csr is a renewal spoke cluster csr.
	isRenewal := isSpokeClusterClientCertRenewal(csrInfo)
	if !isRenewal {
		klog.V(4).Infof("CSR %q was not recognized", csr.Name)
		return nil
	}

	allowed, err := authorize(ctx, c.kubeClient, csrInfo)
	if err != nil {
		return err
	}
	if !allowed {
		//TODO find a way to avoid looking at this CSR again.
		klog.V(4).Infof("Managed cluster csr %q cannont be auto approved due to subject access review was not approved", csr.Name)
		return nil
	}

	// Auto approve the spoke cluster csr
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type:    certificatesv1beta1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
	})
	_, err = c.kubeClient.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(ctx, csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	c.eventRecorder.Eventf("ManagedClusterCSRAutoApproved", "spoke cluster csr %q is auto approved by hub csr controller", csr.Name)
	return nil
}
