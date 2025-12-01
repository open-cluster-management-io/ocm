package csr

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/registration/hub/user"
)

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

type CSRInfo struct {
	Name       string
	Labels     map[string]string
	SignerName string
	Username   string
	UID        string
	Groups     []string
	Extra      map[string]authorizationv1.ExtraValue
	Request    []byte
}

type approveCSRFunc func(kubernetes.Interface) error

type Reconciler interface {
	Reconcile(context.Context, factory.SyncContext, CSRInfo, approveCSRFunc) (reconcileState, error)
}

type csrRenewalReconciler struct {
	signer     string
	kubeClient kubernetes.Interface
}

func NewCSRRenewalReconciler(kubeClient kubernetes.Interface, signer string) Reconciler {
	return &csrRenewalReconciler{
		signer:     signer,
		kubeClient: kubeClient,
	}
}

func (r *csrRenewalReconciler) Reconcile(ctx context.Context, syncCtx factory.SyncContext, csr CSRInfo, approveCSR approveCSRFunc) (reconcileState, error) {
	logger := klog.FromContext(ctx)
	// Check whether current csr is a valid spoker cluster csr.
	valid, _, commonName := validateCSR(logger, r.signer, csr)
	if !valid {
		logger.V(4).Info("CSR was not recognized", "csrName", csr.Name)
		return reconcileStop, nil
	}

	// Check if user name in csr is the same as commonName field in csr request.
	if csr.Username != commonName {
		return reconcileContinue, nil
	}

	// Authorize whether the current spoke agent has been authorized to renew its csr.
	allowed, err := authorize(ctx, r.kubeClient, csr)
	if err != nil {
		return reconcileContinue, err
	}
	if !allowed {
		logger.V(4).Info("Managed cluster csr cannot be auto approved due to subject access review not approved", "csrName", csr.Name)
		return reconcileStop, nil
	}

	if err := approveCSR(r.kubeClient); err != nil {
		return reconcileContinue, err
	}

	syncCtx.Recorder().Eventf(ctx, "ManagedClusterCSRAutoApproved", "managed cluster csr %q is auto approved by hub csr controller", csr.Name)
	return reconcileStop, nil
}

type csrBootstrapReconciler struct {
	signer        string
	kubeClient    kubernetes.Interface
	approvalUsers sets.Set[string]
}

func NewCSRBootstrapReconciler(kubeClient kubernetes.Interface,
	signer string,
	approvalUsers []string) Reconciler {
	return &csrBootstrapReconciler{
		signer:        signer,
		kubeClient:    kubeClient,
		approvalUsers: sets.New(approvalUsers...),
	}
}

func (b *csrBootstrapReconciler) Reconcile(ctx context.Context, syncCtx factory.SyncContext, csr CSRInfo, approveCSR approveCSRFunc) (reconcileState, error) {
	logger := klog.FromContext(ctx)
	// Check whether current csr is a valid spoker cluster csr.
	valid, clusterName, _ := validateCSR(logger, b.signer, csr)
	if !valid {
		logger.V(4).Info("CSR was not recognized", "csrName", csr.Name)
		return reconcileStop, nil
	}

	// Check whether current csr can be approved.
	if !b.approvalUsers.Has(csr.Username) {
		return reconcileContinue, nil
	}

	if err := approveCSR(b.kubeClient); err != nil {
		return reconcileContinue, err
	}

	syncCtx.Recorder().Eventf(ctx, "ManagedClusterAutoApproved", "managed cluster %q is auto approved.", clusterName)
	return reconcileStop, nil
}

// To validate a managed cluster csr, we check
// 1. if the signer name in csr request is valid.
// 2. if organization field and commonName field in csr request is valid.
func validateCSR(logger klog.Logger, signer string, csr CSRInfo) (bool, string, string) {
	spokeClusterName, existed := csr.Labels[clusterv1.ClusterNameLabelKey]
	if !existed {
		return false, "", ""
	}

	if csr.SignerName != signer {
		return false, "", ""
	}

	block, _ := pem.Decode(csr.Request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		logger.V(4).Info("CSR was not recognized: PEM block type is not CERTIFICATE REQUEST", "csrName", csr.Name)
		return false, "", ""
	}

	x509cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		logger.Error(err, "CSR was not recognized", "csrName", csr.Name)
		return false, "", ""
	}

	requestingOrgs := sets.New(x509cr.Subject.Organization...)
	if requestingOrgs.Has(user.ManagedClustersGroup) { // optional common group for backward-compatibility
		requestingOrgs.Delete(user.ManagedClustersGroup)
	}
	if requestingOrgs.Len() != 1 {
		return false, "", ""
	}

	expectedPerClusterOrg := fmt.Sprintf("%s%s", user.SubjectPrefix, spokeClusterName)
	if !requestingOrgs.Has(expectedPerClusterOrg) {
		return false, "", ""
	}

	if !strings.HasPrefix(x509cr.Subject.CommonName, expectedPerClusterOrg) {
		return false, "", ""
	}

	return true, spokeClusterName, x509cr.Subject.CommonName
}

// Using SubjectAccessReview API to check whether a spoke agent has been authorized to renew its csr,
// a spoke agent is authorized after its spoke cluster is accepted by hub cluster admin.
func authorize(ctx context.Context, kubeClient kubernetes.Interface, csr CSRInfo) (bool, error) {
	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   csr.Username,
			UID:    csr.UID,
			Groups: csr.Groups,
			Extra:  csr.Extra,
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

func getCSRInfo(csr *certificatesv1.CertificateSigningRequest) CSRInfo {
	extra := make(map[string]authorizationv1.ExtraValue)
	for k, v := range csr.Spec.Extra {
		extra[k] = authorizationv1.ExtraValue(v)
	}
	return CSRInfo{
		Name:       csr.Name,
		Labels:     csr.Labels,
		SignerName: csr.Spec.SignerName,
		Username:   csr.Spec.Username,
		UID:        csr.Spec.UID,
		Groups:     csr.Spec.Groups,
		Extra:      extra,
		Request:    csr.Spec.Request,
	}
}

func getCSRv1beta1Info(csr *certificatesv1beta1.CertificateSigningRequest) CSRInfo {
	extra := make(map[string]authorizationv1.ExtraValue)
	for k, v := range csr.Spec.Extra {
		extra[k] = authorizationv1.ExtraValue(v)
	}
	return CSRInfo{
		Name:       csr.Name,
		Labels:     csr.Labels,
		SignerName: *csr.Spec.SignerName,
		Username:   csr.Spec.Username,
		UID:        csr.Spec.UID,
		Groups:     csr.Spec.Groups,
		Extra:      extra,
		Request:    csr.Spec.Request,
	}
}

func eventFilter(csr any) bool {
	switch v := csr.(type) {
	case *certificatesv1.CertificateSigningRequest:
		return v.Spec.SignerName == certificatesv1.KubeAPIServerClientSignerName
	case *certificatesv1beta1.CertificateSigningRequest:
		return *v.Spec.SignerName == certificatesv1beta1.KubeAPIServerClientSignerName
	default:
		return false
	}
}
