package csr

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"github.com/open-cluster-management/registration/pkg/helpers"
)

const (
	subjectPrefix         = "system:open-cluster-management:"
	spokeClusterNameLabel = "open-cluster-management.io/cluster-name"
)

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController struct {
	kubeClient    kubernetes.Interface
	eventRecorder events.Recorder
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController(kubeClient kubernetes.Interface, csrInformer factory.Informer, recorder events.Recorder) factory.Controller {
	c := &csrApprovingController{
		kubeClient:    kubeClient,
		eventRecorder: recorder.WithComponentSuffix("csr-approving-controller"),
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, csrInformer).
		WithSync(c.sync).
		ToController("CSRApprovingController", recorder)
}

func (c *csrApprovingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling CertificateSigningRequests %q", csrName)
	csr, err := c.kubeClient.CertificatesV1beta1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// Current csr is in terminal state, do nothing.
	if helpers.IsCSRInTerminalState(&csr.Status) {
		return nil
	}

	// Check whether current csr is a renewal spoker cluster csr.
	isRenewal := isSpokeClusterClientCertRenewal(csr)
	if !isRenewal {
		klog.V(4).Infof("CSR %q was not recognized", csr.Name)
		return nil
	}

	// Authorize whether the current spoke agent has been authorized to renew its csr.
	allowed, err := c.authorize(ctx, csr)
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

// Using SubjectAccessReview API to check whether a spoke agent has been authorized to renew its csr,
// a spoke agent is authorized after its spoke cluster is accepted by hub cluster admin.
func (c *csrApprovingController) authorize(ctx context.Context, csr *certificatesv1beta1.CertificateSigningRequest) (bool, error) {
	extra := make(map[string]authorizationv1.ExtraValue)
	for k, v := range csr.Spec.Extra {
		extra[k] = authorizationv1.ExtraValue(v)
	}

	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   csr.Spec.Username,
			UID:    csr.Spec.UID,
			Groups: csr.Spec.Groups,
			Extra:  extra,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:       "register.open-cluster-management.io",
				Resource:    "managedclusters",
				Verb:        "renew",
				Subresource: "clientcertificates",
			},
		},
	}
	sar, err := c.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	return sar.Status.Allowed, nil
}

// To check a renewal managed cluster csr, we check
// 1. if the signer name in csr request is valid.
// 2. if organization field and commonName field in csr request is valid.
// 3. if user name in csr is the same as commonName field in csr request.
func isSpokeClusterClientCertRenewal(csr *certificatesv1beta1.CertificateSigningRequest) bool {
	spokeClusterName, existed := csr.Labels[spokeClusterNameLabel]
	if !existed {
		return false
	}

	// The CSR signer name must be provided on Kubernetes v1.18.0 and above, so if the signer name is empty,
	// we should be on an old server, we skip the signer name check
	if (csr.Spec.SignerName != nil && len(*csr.Spec.SignerName) != 0) &&
		*csr.Spec.SignerName != certificatesv1beta1.KubeAPIServerClientSignerName {
		return false
	}

	block, _ := pem.Decode(csr.Spec.Request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		klog.V(4).Infof("csr %q was not recognized: PEM block type is not CERTIFICATE REQUEST", csr.Name)
		return false
	}

	x509cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		klog.V(4).Infof("csr %q was not recognized: %v", csr.Name, err)
		return false
	}

	if len(x509cr.Subject.Organization) != 1 {
		return false
	}

	organization := x509cr.Subject.Organization[0]
	if organization != fmt.Sprintf("%s%s", subjectPrefix, spokeClusterName) {
		return false
	}

	if !strings.HasPrefix(x509cr.Subject.CommonName, organization) {
		return false
	}

	return csr.Spec.Username == x509cr.Subject.CommonName
}
