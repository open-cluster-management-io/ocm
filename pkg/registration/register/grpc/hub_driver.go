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
	certificatesv1informers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificatesv1listers "k8s.io/client-go/listers/certificates/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	sdkhelpers "open-cluster-management.io/sdk-go/pkg/helpers"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type GRPCHubDriver struct {
	controller factory.Controller
}

func (c *GRPCHubDriver) Run(ctx context.Context, workers int) {
	c.controller.Run(ctx, workers)
}

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
	return &GRPCHubDriver{
		controller: newCSRSignController(
			kubeClient,
			kubeInformers.Certificates().V1().CertificateSigningRequests(),
			caKey, caData, duration, recorder,
		),
	}, nil
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
	csrLister  certificatesv1listers.CertificateSigningRequestLister
	caKey      []byte
	caData     []byte
	duration   time.Duration
}

// newCSRSignController creates a new csr signing controller
func newCSRSignController(
	kubeClient kubernetes.Interface,
	csrInformer certificatesv1informers.CertificateSigningRequestInformer,
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
		if condition.Type == certificatesv1.CertificateApproved {
			approved = true
			break
		}
	}

	if !approved {
		return nil
	}

	if len(csr.Status.Certificate) > 0 {
		return nil
	}

	// Do not sign apiserver cert
	if csr.Spec.SignerName != helpers.GRPCCAuthSigner {
		return nil
	}

	signerFunc := sdkhelpers.CSRSignerWithExpiry(c.caKey, c.caData, c.duration)
	csr.Status.Certificate = signerFunc(csr)
	if len(csr.Status.Certificate) == 0 {
		return fmt.Errorf("invalid client certificate generated for csr %q", csr.Name)
	}
	_, err = c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateStatus(ctx, csr, metav1.UpdateOptions{})
	return err
}
