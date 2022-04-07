package clientcert

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	certificates "k8s.io/api/certificates/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1beta1"
	csrclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	certificateslisters "k8s.io/client-go/listers/certificates/v1beta1"
	"k8s.io/client-go/tools/cache"
)

var _ csrControl = &v1beta1CSRControl{}

type v1beta1CSRControl struct {
	hubCSRInformer certificatesinformers.CertificateSigningRequestInformer
	hubCSRLister   certificateslisters.CertificateSigningRequestLister
	hubCSRClient   csrclient.CertificateSigningRequestInterface
}

func (v *v1beta1CSRControl) isApproved(name string) (bool, error) {
	csr, err := v.get(name)
	if err != nil {
		return false, err
	}
	v1beta1CSR := csr.(*certificates.CertificateSigningRequest)
	approved := false
	for _, condition := range v1beta1CSR.Status.Conditions {
		if condition.Type == certificates.CertificateDenied {
			return false, nil
		} else if condition.Type == certificates.CertificateApproved {
			approved = true
		}
	}
	return approved, nil
}

func (v *v1beta1CSRControl) getIssuedCertificate(name string) ([]byte, error) {
	csr, err := v.get(name)
	if err != nil {
		return nil, err
	}
	v1beta1CSR := csr.(*certificates.CertificateSigningRequest)
	// skip if csr has no certificate in its status yet
	if len(v1beta1CSR.Status.Certificate) == 0 {
		return nil, nil
	}
	return v1beta1CSR.Status.Certificate, nil
}

func (v *v1beta1CSRControl) create(ctx context.Context, recorder events.Recorder, objMeta metav1.ObjectMeta, csrData []byte, signerName string) (string, error) {
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: objMeta,
		Spec: certificates.CertificateSigningRequestSpec{
			Request: csrData,
			Usages: []certificates.KeyUsage{
				certificates.UsageDigitalSignature,
				certificates.UsageKeyEncipherment,
				certificates.UsageClientAuth,
			},
			SignerName: &signerName,
		},
	}

	req, err := v.hubCSRClient.Create(ctx, csr, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	recorder.Eventf("CSRCreated", "A csr %q is created", req.Name)
	return req.Name, nil
}

func (v *v1beta1CSRControl) informer() cache.SharedIndexInformer {
	return v.hubCSRInformer.Informer()
}

func (v *v1beta1CSRControl) get(name string) (metav1.Object, error) {
	csr, err := v.hubCSRLister.Get(name)
	switch {
	case apierrors.IsNotFound(err):
		// fallback to fetching csr from hub apiserver in case it is not cached by informer yet
		csr, err = v.hubCSRClient.Get(context.Background(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("unable to get csr %q. It might have already been deleted", name)
		}
	case err != nil:
		return nil, err
	}
	return csr, nil
}
