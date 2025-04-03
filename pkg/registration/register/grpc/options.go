package grpc

import (
	"context"
	"errors"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/pflag"
	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	cloudeventscsr "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"

	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

const signer = "open-cluster-management.io/grpc"

type Option struct {
	BootstrapConfigFile string
	ConfigFile          string
	csrOption           *csr.CSROption
	grpcConfig          []byte
}

func NewOptions() *Option {
	return &Option{}
}

func (o *Option) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.BootstrapConfigFile, "grpc-bootstrap-config", o.BootstrapConfigFile, "")
	fs.StringVar(&o.ConfigFile, "grpc-config", o.ConfigFile, "")
}

func (o *Option) Validate() error {
	if o.ConfigFile == "" && o.BootstrapConfigFile == "" {
		return errors.New("config file should be set")
	}
	return nil
}

var _ csr.CSRControl = &csrControl{}

type csrControl struct {
	csrClientHolder *cloudeventscsr.ClientHolder
}

func (v *csrControl) IsApproved(name string) (bool, error) {
	csr, err := v.csrClientHolder.Clients().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	approved := false
	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificates.CertificateDenied {
			return false, nil
		} else if condition.Type == certificates.CertificateApproved {
			approved = true
		}
	}
	return approved, nil
}

func (v *csrControl) GetIssuedCertificate(name string) ([]byte, error) {
	csr, err := v.csrClientHolder.Clients().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return csr.Status.Certificate, nil
}

func (v *csrControl) Create(ctx context.Context, recorder events.Recorder, objMeta metav1.ObjectMeta, csrData []byte,
	signerName string, expirationSeconds *int32) (string, error) {
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: objMeta,
		Spec: certificates.CertificateSigningRequestSpec{
			Request: csrData,
			Usages: []certificates.KeyUsage{
				certificates.UsageDigitalSignature,
				certificates.UsageKeyEncipherment,
				certificates.UsageClientAuth,
			},
			SignerName:        signerName,
			ExpirationSeconds: expirationSeconds,
		},
	}

	req, err := v.csrClientHolder.Clients().Create(ctx, csr, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	recorder.Eventf("CSRCreated", "A csr %q is created", req.Name)
	return req.Name, nil
}

func (v *csrControl) Informer() cache.SharedIndexInformer {
	return v.csrClientHolder.Informer()
}
