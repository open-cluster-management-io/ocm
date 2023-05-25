package csr

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/operator/events"

	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/hub/user"
)

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

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

type approveCSRFunc func(kubernetes.Interface) error

type Reconciler interface {
	Reconcile(context.Context, csrInfo, approveCSRFunc) (reconcileState, error)
}

type csrRenewalReconciler struct {
	kubeClient    kubernetes.Interface
	eventRecorder events.Recorder
}

func NewCSRRenewalReconciler(kubeClient kubernetes.Interface, recorder events.Recorder) Reconciler {
	return &csrRenewalReconciler{
		kubeClient:    kubeClient,
		eventRecorder: recorder.WithComponentSuffix("csr-approving-controller"),
	}
}

func (r *csrRenewalReconciler) Reconcile(ctx context.Context, csr csrInfo, approveCSR approveCSRFunc) (reconcileState, error) {
	// Check whether current csr is a valid spoker cluster csr.
	valid, _, commonName := validateCSR(csr)
	if !valid {
		klog.V(4).Infof("CSR %q was not recognized", csr.name)
		return reconcileStop, nil
	}

	// Check if user name in csr is the same as commonName field in csr request.
	if csr.username != commonName {
		return reconcileContinue, nil
	}

	// Authorize whether the current spoke agent has been authorized to renew its csr.
	allowed, err := authorize(ctx, r.kubeClient, csr)
	if err != nil {
		return reconcileContinue, err
	}
	if !allowed {
		klog.V(4).Infof("Managed cluster csr %q cannont be auto approved due to subject access review was not approved", csr.name)
		return reconcileStop, nil
	}

	if err := approveCSR(r.kubeClient); err != nil {
		return reconcileContinue, err
	}

	r.eventRecorder.Eventf("ManagedClusterCSRAutoApproved", "spoke cluster csr %q is auto approved by hub csr controller", csr.name)
	return reconcileStop, nil
}

type csrBootstrapReconciler struct {
	kubeClient    kubernetes.Interface
	clusterClient clusterclientset.Interface
	clusterLister clusterv1listers.ManagedClusterLister
	approvalUsers sets.Set[string]
	eventRecorder events.Recorder
}

func NewCSRBootstrapReconciler(kubeClient kubernetes.Interface,
	clusterClient clusterclientset.Interface,
	clusterLister clusterv1listers.ManagedClusterLister,
	approvalUsers []string,
	recorder events.Recorder) Reconciler {
	return &csrBootstrapReconciler{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		clusterLister: clusterLister,
		approvalUsers: sets.New(approvalUsers...),
		eventRecorder: recorder.WithComponentSuffix("csr-approving-controller"),
	}
}

func (b *csrBootstrapReconciler) Reconcile(ctx context.Context, csr csrInfo, approveCSR approveCSRFunc) (reconcileState, error) {
	// Check whether current csr is a valid spoker cluster csr.
	valid, clusterName, _ := validateCSR(csr)
	if !valid {
		klog.V(4).Infof("CSR %q was not recognized", csr.name)
		return reconcileStop, nil
	}

	// Check whether current csr can be approved.
	if !b.approvalUsers.Has(csr.username) {
		return reconcileContinue, nil
	}

	err := b.accpetCluster(ctx, clusterName)
	if errors.IsNotFound(err) {
		// Current spoke cluster not found, could have been deleted, do nothing.
		return reconcileStop, nil
	}
	if err != nil {
		return reconcileContinue, err
	}

	if err := approveCSR(b.kubeClient); err != nil {
		return reconcileContinue, err
	}

	b.eventRecorder.Eventf("ManagedClusterAutoApproved", "spoke cluster %q is auto approved.", clusterName)
	return reconcileStop, nil
}

func (b *csrBootstrapReconciler) accpetCluster(ctx context.Context, managedClusterName string) error {
	managedCluster, err := b.clusterLister.Get(managedClusterName)
	if err != nil {
		return err
	}

	if managedCluster.Spec.HubAcceptsClient {
		return nil
	}

	patch := []byte("{\"spec\": {\"hubAcceptsClient\": true}}")
	_, err = b.clusterClient.ClusterV1().ManagedClusters().Patch(
		ctx, managedCluster.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// To validate a managed cluster csr, we check
// 1. if the signer name in csr request is valid.
// 2. if organization field and commonName field in csr request is valid.
func validateCSR(csr csrInfo) (bool, string, string) {
	spokeClusterName, existed := csr.labels[clusterv1.ClusterNameLabelKey]
	if !existed {
		return false, "", ""
	}

	if csr.signerName != certificatesv1.KubeAPIServerClientSignerName {
		return false, "", ""
	}

	block, _ := pem.Decode(csr.request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		klog.V(4).Infof("csr %q was not recognized: PEM block type is not CERTIFICATE REQUEST", csr.name)
		return false, "", ""
	}

	x509cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		klog.V(4).Infof("csr %q was not recognized: %v", csr.name, err)
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
