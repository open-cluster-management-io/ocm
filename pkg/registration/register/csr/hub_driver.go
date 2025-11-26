package csr

import (
	"context"
	"fmt"

	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type CSR interface {
	*certificatesv1.CertificateSigningRequest | *certificatesv1beta1.CertificateSigningRequest
}

type CSRInfoGetter[T CSR] func(c T) CSRInfo

type CSRLister[T CSR] interface {
	Get(name string) (T, error)
}

type csrApprover[T CSR] interface {
	approve(ctx context.Context, csr T) approveCSRFunc
	isInTerminalState(csr T) bool
}

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController[T CSR] struct {
	lister        CSRLister[T]
	approver      csrApprover[T]
	csrInfoGetter CSRInfoGetter[T]
	reconcilers   []Reconciler
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController[T CSR](
	csrInformer cache.SharedIndexInformer,
	lister CSRLister[T],
	eventFilter factory.EventFilterFunc,
	approver csrApprover[T],
	csrInfoGetter CSRInfoGetter[T],
	reconcilers []Reconciler) factory.Controller {
	c := &csrApprovingController[T]{
		lister:        lister,
		approver:      approver,
		csrInfoGetter: csrInfoGetter,
		reconcilers:   reconcilers,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(queue.QueueKeyByMetaName, eventFilter, csrInformer).
		WithSync(c.sync).
		ToController("CSRApprovingController")
}

func (c *csrApprovingController[T]) sync(ctx context.Context, syncCtx factory.SyncContext, csrName string) error {
	logger := klog.FromContext(ctx).WithValues("csrName", csrName)
	logger.V(4).Info("Reconciling CertificateSigningRequests")
	ctx = klog.NewContext(ctx, logger)

	csr, err := c.lister.Get(csrName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if c.approver.isInTerminalState(csr) {
		return nil
	}

	csrInfo := c.csrInfoGetter(csr)
	for _, r := range c.reconcilers {
		state, err := r.Reconcile(ctx, syncCtx, csrInfo, c.approver.approve(ctx, csr))
		if err != nil {
			return err
		}
		if state == reconcileStop {
			break
		}
	}

	return nil
}

var _ csrApprover[*certificatesv1.CertificateSigningRequest] = &csrV1Approver{}

// CSRV1Approver implement CSRApprover interface
type csrV1Approver struct {
	kubeClient kubernetes.Interface
}

func NewCSRV1Approver(client kubernetes.Interface) *csrV1Approver {
	return &csrV1Approver{kubeClient: client}
}

func (c *csrV1Approver) isInTerminalState(csr *certificatesv1.CertificateSigningRequest) bool { //nolint:unused
	return helpers.IsCSRInTerminalState(&csr.Status)
}

func (c *csrV1Approver) approve(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) approveCSRFunc { //nolint:unused
	return func(kubeClient kubernetes.Interface) error {
		csrCopy := csr.DeepCopy()
		// Auto approve the spoke cluster csr
		csrCopy.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{ //nolint:gocritic
			Type:    certificatesv1.CertificateApproved,
			Status:  corev1.ConditionTrue,
			Reason:  "AutoApprovedByHubCSRApprovingController",
			Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
		})
		_, err := kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csrCopy.Name, csrCopy, metav1.UpdateOptions{})
		return err
	}
}

var _ csrApprover[*certificatesv1beta1.CertificateSigningRequest] = &csrV1beta1Approver{}

type csrV1beta1Approver struct {
	kubeClient kubernetes.Interface
}

func newCSRV1beta1Approver(client kubernetes.Interface) *csrV1beta1Approver {
	return &csrV1beta1Approver{kubeClient: client}
}

func (c *csrV1beta1Approver) isInTerminalState(csr *certificatesv1beta1.CertificateSigningRequest) bool { //nolint:unused
	return helpers.Isv1beta1CSRInTerminalState(&csr.Status)
}

func (c *csrV1beta1Approver) approve(ctx context.Context, csr *certificatesv1beta1.CertificateSigningRequest) approveCSRFunc { //nolint:unused
	return func(kubeClient kubernetes.Interface) error {
		csrCopy := csr.DeepCopy()
		// Auto approve the spoke cluster csr
		csrCopy.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{ //nolint:gocritic
			Type:    certificatesv1beta1.CertificateApproved,
			Status:  corev1.ConditionTrue,
			Reason:  "AutoApprovedByHubCSRApprovingController",
			Message: "Auto approving Managed cluster agent certificate after SubjectAccessReview.",
		})
		_, err := kubeClient.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(ctx, csrCopy, metav1.UpdateOptions{})
		return err
	}
}

type CSRHubDriver struct {
	controller factory.Controller
}

func (c *CSRHubDriver) Run(ctx context.Context, workers int) {
	c.controller.Run(ctx, workers)
}

// Cleanup is run when the cluster is deleting or hubAcceptClient is set false
func (c *CSRHubDriver) Cleanup(_ context.Context, _ *clusterv1.ManagedCluster) error {
	// noop
	return nil
}

func NewCSRHubDriver(
	kubeClient kubernetes.Interface,
	kubeInformers informers.SharedInformerFactory,
	autoApprovedCSRUsers []string) (register.HubDriver, error) {
	csrDriverForHub := &CSRHubDriver{}

	csrReconciles := []Reconciler{NewCSRRenewalReconciler(kubeClient, certificatesv1.KubeAPIServerClientSignerName)}
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManagedClusterAutoApproval) {
		csrReconciles = append(csrReconciles, NewCSRBootstrapReconciler(
			kubeClient,
			certificatesv1.KubeAPIServerClientSignerName,
			autoApprovedCSRUsers,
		))
	}

	if features.HubMutableFeatureGate.Enabled(ocmfeature.V1beta1CSRAPICompatibility) {
		v1CSRSupported, v1beta1CSRSupported, err := helpers.IsCSRSupported(kubeClient)
		if err != nil {
			return nil, fmt.Errorf("failed CSR api discovery: %v", err)
		}

		if !v1CSRSupported && v1beta1CSRSupported {
			csrDriverForHub.controller = NewCSRApprovingController(
				kubeInformers.Certificates().V1beta1().CertificateSigningRequests().Informer(),
				kubeInformers.Certificates().V1beta1().CertificateSigningRequests().Lister(),
				eventFilter,
				newCSRV1beta1Approver(kubeClient),
				getCSRv1beta1Info,
				csrReconciles,
			)
			return csrDriverForHub, nil
		}
	}

	csrDriverForHub.controller = NewCSRApprovingController(
		kubeInformers.Certificates().V1().CertificateSigningRequests().Informer(),
		kubeInformers.Certificates().V1().CertificateSigningRequests().Lister(),
		eventFilter,
		NewCSRV1Approver(kubeClient),
		getCSRInfo,
		csrReconciles,
	)

	return csrDriverForHub, nil
}

func (a *CSRHubDriver) CreatePermissions(_ context.Context, _ *clusterv1.ManagedCluster) error {
	// noop
	return nil
}

func (c *CSRHubDriver) Accept(_ *clusterv1.ManagedCluster) bool {
	return true
}
