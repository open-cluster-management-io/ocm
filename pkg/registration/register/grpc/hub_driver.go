package grpc

import (
	"fmt"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"golang.org/x/net/context"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificateslisters "k8s.io/client-go/listers/certificates/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/helpers"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

type GRPCHubDriver struct {
	controller factory.Controller
}

func (c *GRPCHubDriver) Run(ctx context.Context, workers int) {
	c.controller.Run(ctx, workers)
}

// Cleanup is run when the cluster is deleting or hubAcceptClient is set false
func (c *GRPCHubDriver) Cleanup(_ context.Context, _ *clusterv1.ManagedCluster) error {
	// noop
	return nil
}

func NewGRPCHubDriver(
	kubeClient kubernetes.Interface,
	kubeInformers informers.SharedInformerFactory,
	caKeyFile, caFile string,
	duration time.Duration,
	recorder events.Recorder) (register.HubDriver, error) {
	caData, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	caKey, err := os.ReadFile(caKeyFile)
	if err != nil {
		return nil, err
	}
	grpcDriverForHub := &GRPCHubDriver{}
	grpcDriverForHub.controller = newCSRSignController(
		kubeClient,
		kubeInformers.Certificates().V1().CertificateSigningRequests(),
		caKey, caData, duration, recorder,
	)

	return grpcDriverForHub, nil
}

func (a *GRPCHubDriver) CreatePermissions(_ context.Context, _ *clusterv1.ManagedCluster) error {
	// noop
	return nil
}

func (c *GRPCHubDriver) Accept(_ *clusterv1.ManagedCluster) bool {
	return true
}

type csrSignController struct {
	kubeClient kubernetes.Interface
	csrLister  certificateslisters.CertificateSigningRequestLister
	caKey      []byte
	caData     []byte
	duration   time.Duration
}

// NewCSRApprovingController creates a new csr approving controller
func newCSRSignController(
	kubeClient kubernetes.Interface,
	csrInformer certificatesinformers.CertificateSigningRequestInformer,
	caKey, caData []byte,
	duration time.Duration,
	recorder events.Recorder,
) factory.Controller {
	c := &csrSignController{
		kubeClient: kubeClient,
		csrLister:  csrInformer.Lister(),
		caKey:      caKey,
		caData:     caData,
		duration:   duration,
	}
	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				accessor, _ := meta.Accessor(obj)
				return []string{accessor.GetName()}
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				if len(accessor.GetLabels()) == 0 {
					return false
				}
				labels := accessor.GetLabels()
				if _, ok := labels[clusterv1.ClusterNameLabelKey]; !ok {
					return false
				}
				return true
			},
			csrInformer.Informer()).
		WithSync(c.sync).
		ToController("CSRSignController", recorder)
}

func (c *csrSignController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()
	csr, err := c.csrLister.Get(csrName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	csr = csr.DeepCopy()

	approved := false
	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificatesv1.CertificateDenied {
			return nil
		} else if condition.Type == certificatesv1.CertificateApproved {
			approved = true
		}
	}
	if !approved {
		return nil
	}

	if len(csr.Status.Certificate) > 0 {
		return nil
	}

	// Do not sigh apiserver cert
	if csr.Spec.SignerName != signer {
		return nil
	}

	signerFunc := helpers.CSRSignerWithExpiry(c.caKey, c.caData, c.duration)
	csr.Status.Certificate = signerFunc(csr)
	if len(csr.Status.Certificate) == 0 {
		return fmt.Errorf("invalid client certificate generated for addon csr %q", csr.Name)
	}
	_, err = c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateStatus(ctx, csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}
