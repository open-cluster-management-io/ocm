package csr

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/open-cluster-management/registration/pkg/hub/csr/bindata"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const (
	manifestDir   = "pkg/hub/csr"
	subjectPrefix = "system:open-cluster-management:"
)

// csrController reconciles instances of spoke cluster CertificateSigningRequests on the hub.
type csrController struct {
	kubeClient    kubernetes.Interface
	eventRecorder events.Recorder
}

// NewCSRController creates a new csr controller
func NewCSRController(kubeClient kubernetes.Interface, csrInformer factory.Informer, recorder events.Recorder) factory.Controller {
	c := &csrController{
		kubeClient:    kubeClient,
		eventRecorder: recorder.WithComponentSuffix("csr-controller"),
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, csrInformer).
		WithSync(c.sync).
		ToController("CSRController", recorder)
}

func (c *csrController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling CertificateSigningRequests %q", csrName)
	csr, err := c.kubeClient.CertificatesV1beta1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// Current csr is denied, do nothing.
	approved, denied := getCertApprovalCondition(&csr.Status)
	if denied {
		return nil
	}

	// Recognize whether current csr is a spoker clsuter csr.
	recognized, spokeClusterName := recognize(csr)
	if !recognized {
		klog.V(4).Infof("CSR %q was not recognized", csr.Name)
		return nil
	}

	// Current spoker clsuter csr is approved, apply the corresponding clusterrole/clusterrolebinding.
	if approved {
		resourceResults := resourceapply.ApplyDirectly(
			resourceapply.NewKubeClientHolder(c.kubeClient),
			syncCtx.Recorder(),
			func(name string) ([]byte, error) {
				config := struct {
					SpokeClusterName string
				}{
					SpokeClusterName: spokeClusterName,
				}
				return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join(manifestDir, name)), config).Data, nil
			},
			"manifests/clusterrole.yaml",
			"manifests/clusterrolebinding.yaml",
		)
		errs := []error{}
		for _, result := range resourceResults {
			if result.Error != nil {
				errs = append(errs, fmt.Errorf("%q (%T): %w", result.File, result.Type, result.Error))
			}
		}
		return operatorhelpers.NewMultiLineAggregate(errs)
	}

	// Using SubjectAccessReview API to authorize whether the current spoke agent has been authorized to renew its csr.
	// A spoke agent is authorized after its cluster is accepted by hub cluster admin.
	allowed, err := c.authorize(ctx, csr)
	if err != nil {
		return err
	}
	if !allowed {
		klog.V(4).Infof("Spoke cluster csr %q cannont be auto approved due to subject access review was not approved", csr.Name)
		return nil
	}

	// Auto approve the spoker clsuter csr
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type:    certificatesv1beta1.CertificateApproved,
		Reason:  "AutoApproved",
		Message: "Auto approving spoke cluster agent certificate after SubjectAccessReview.",
	})
	_, err = c.kubeClient.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(ctx, csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	c.eventRecorder.Eventf("SpokeClusterCSRAutoApproved", "spoke cluster csr %q is auto approved by hub csr controller", csr.Name)
	return nil
}

func (c *csrController) authorize(ctx context.Context, csr *certificatesv1beta1.CertificateSigningRequest) (bool, error) {
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
				Group:       "certificates.k8s.io",
				Resource:    "certificatesigningrequests",
				Verb:        "create",
				Subresource: "spokeclusteragent",
			},
		},
	}
	sar, err := c.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	return sar.Status.Allowed, nil
}

// To recognize a valid spoke cluster csr, we check
// 1. if organization field and commonName field in csr request is valid.
// 2. if user name in csr is the same as commonName field in csr request.
func recognize(csr *certificatesv1beta1.CertificateSigningRequest) (recognized bool, spokeClusterName string) {
	block, _ := pem.Decode(csr.Spec.Request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		klog.V(4).Infof("csr %q was not recognized: PEM block type is not CERTIFICATE REQUEST", csr.Name)
		return false, ""
	}

	x509cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		klog.V(4).Infof("csr %q was not recognized: %w", csr.Name, err)
		return false, ""
	}

	if len(x509cr.Subject.Organization) != 1 {
		return false, ""
	}
	organization := x509cr.Subject.Organization[0]
	if !strings.HasPrefix(organization, subjectPrefix) {
		return false, ""
	}

	if !strings.HasPrefix(x509cr.Subject.CommonName, organization) {
		return false, ""
	}

	return csr.Spec.Username == x509cr.Subject.CommonName, strings.TrimPrefix(organization, subjectPrefix)
}

func getCertApprovalCondition(status *certificatesv1beta1.CertificateSigningRequestStatus) (approved, denied bool) {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1beta1.CertificateApproved {
			approved = true
		}
		if c.Type == certificatesv1beta1.CertificateDenied {
			denied = true
		}
	}
	return
}
