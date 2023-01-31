package csr

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificateslisters "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/klog/v2"
	"open-cluster-management.io/registration/pkg/helpers"
	"open-cluster-management.io/registration/pkg/hub/user"
)

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController struct {
	kubeClient    kubernetes.Interface
	csrLister     certificateslisters.CertificateSigningRequestLister
	eventRecorder events.Recorder
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController(kubeClient kubernetes.Interface, csrInformer certificatesinformers.CertificateSigningRequestInformer, recorder events.Recorder) factory.Controller {
	c := &csrApprovingController{
		kubeClient:    kubeClient,
		csrLister:     csrInformer.Lister(),
		eventRecorder: recorder.WithComponentSuffix("csr-approving-controller"),
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
	// Check whether current csr is a renewal spoker cluster csr.
	isRenewal := isSpokeClusterClientCertRenewal(csrInfo)
	if !isRenewal {
		klog.V(4).Infof("CSR %q was not recognized", csr.Name)
		return nil
	}

	// Authorize whether the current spoke agent has been authorized to renew its csr.
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
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:    certificatesv1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
	})
	_, err = c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	c.eventRecorder.Eventf("ManagedClusterCSRAutoApproved", "spoke cluster csr %q is auto approved by hub csr controller", csr.Name)
	return nil
}

// Using SubjectAccessReview API to check whether a spoke agent has been authorized to renew its csr,
// a spoke agent is authorized after its spoke cluster is accepted by hub cluster admin.
func authorize(ctx context.Context, kubeClient kubernetes.Interface, csr csrInfo) (bool, error) {
	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   csr.username,
			UID:    csr.uid,
			Groups: csr.groups,
			Extra:  csr.extra,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:       "register.open-cluster-management.io",
				Resource:    "managedclusters",
				Verb:        "renew",
				Subresource: "clientcertificates",
			},
		},
	}

	sar, err := kubeClient.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	return sar.Status.Allowed, nil
}

// To check a renewal managed cluster csr, we check
// 1. if the signer name in csr request is valid.
// 2. if organization field and commonName field in csr request is valid.
// 3. if user name in csr is the same as commonName field in csr request.
func isSpokeClusterClientCertRenewal(csr csrInfo) bool {
	spokeClusterName, existed := csr.labels[clusterv1.ClusterNameLabelKey]
	if !existed {
		return false
	}

	if csr.signerName != certificatesv1.KubeAPIServerClientSignerName {
		return false
	}

	block, _ := pem.Decode(csr.request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		klog.V(4).Infof("csr %q was not recognized: PEM block type is not CERTIFICATE REQUEST", csr.name)
		return false
	}

	x509cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		klog.V(4).Infof("csr %q was not recognized: %v", csr.name, err)
		return false
	}

	requestingOrgs := sets.NewString(x509cr.Subject.Organization...)
	if requestingOrgs.Has(user.ManagedClustersGroup) { // optional common group for backward-compatibility
		requestingOrgs.Delete(user.ManagedClustersGroup)
	}
	if requestingOrgs.Len() != 1 {
		return false
	}

	expectedPerClusterOrg := fmt.Sprintf("%s%s", user.SubjectPrefix, spokeClusterName)
	if !requestingOrgs.Has(expectedPerClusterOrg) {
		return false
	}

	if !strings.HasPrefix(x509cr.Subject.CommonName, expectedPerClusterOrg) {
		return false
	}

	return csr.username == x509cr.Subject.CommonName
}

type csrInfo struct {
	name       string
	labels     map[string]string
	signerName string
	username   string
	uid        string
	groups     []string
	extra      map[string]authorizationv1.ExtraValue
	request    []byte
}

// newCSRInfo creates csrInfo from CertificateSigningRequest by api version(v1/v1beta1).
func newCSRInfo(csr any) csrInfo {
	extra := make(map[string]authorizationv1.ExtraValue)
	switch v := csr.(type) {
	case *certificatesv1.CertificateSigningRequest:
		for k, v := range v.Spec.Extra {
			extra[k] = authorizationv1.ExtraValue(v)
		}
		return csrInfo{
			name:       v.Name,
			labels:     v.Labels,
			signerName: v.Spec.SignerName,
			username:   v.Spec.Username,
			uid:        v.Spec.UID,
			groups:     v.Spec.Groups,
			extra:      extra,
			request:    v.Spec.Request,
		}
	case *certificatesv1beta1.CertificateSigningRequest:
		for k, v := range v.Spec.Extra {
			extra[k] = authorizationv1.ExtraValue(v)
		}
		return csrInfo{
			name:       v.Name,
			labels:     v.Labels,
			signerName: *v.Spec.SignerName,
			username:   v.Spec.Username,
			uid:        v.Spec.UID,
			groups:     v.Spec.Groups,
			extra:      extra,
			request:    v.Spec.Request,
		}
	default:
		klog.Errorf("Unsupported type %T", v)
		return csrInfo{}
	}
}
