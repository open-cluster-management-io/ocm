package certrotationcontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/certrotation"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

const (
	signerSecret      = "signer-secret"
	caBundleConfigmap = "ca-bundle-configmap"
	signerNamePrefix  = "cluster-manager-webhook"
)

// Follow the rules below to set the value of SigningCertValidity/TargetCertValidity/ResyncInterval:
//
// 1) SigningCertValidity * 1/5 * 1/5 > ResyncInterval * 2
// 2) TargetCertValidity * 1/5 > ResyncInterval * 2
var SigningCertValidity = time.Hour * 24 * 365
var TargetCertValidity = time.Hour * 24 * 30
var ResyncInterval = time.Minute * 5

// certRotationController does:
//
// 1) continuously create a self-signed signing CA (via SigningRotation).
//    It creates the next one when a given percentage of the validity of the old CA has passed.
// 2) maintain a CA bundle with all not yet expired CA certs.
// 3) continuously create target cert/key pairs signed by the latest signing CA
//    It creates the next one when a given percentage of the validity of the previous cert has
//    passed, or when a new CA has been created.
type certRotationController struct {
	signingRotation      certrotation.SigningRotation
	caBundleRotation     certrotation.CABundleRotation
	targetRotations      []certrotation.TargetRotation
	kubeClient           kubernetes.Interface
	clusterManagerLister operatorlister.ClusterManagerLister
}

func NewCertRotationController(
	kubeClient kubernetes.Interface,
	secretInformer corev1informers.SecretInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	recorder events.Recorder,
) factory.Controller {
	signingRotation := certrotation.SigningRotation{
		Namespace:        helpers.ClusterManagerNamespace,
		Name:             signerSecret,
		SignerNamePrefix: signerNamePrefix,
		Validity:         SigningCertValidity,
		Lister:           secretInformer.Lister(),
		Client:           kubeClient.CoreV1(),
		EventRecorder:    recorder,
	}
	caBundleRotation := certrotation.CABundleRotation{
		Namespace:     helpers.ClusterManagerNamespace,
		Name:          caBundleConfigmap,
		Lister:        configMapInformer.Lister(),
		Client:        kubeClient.CoreV1(),
		EventRecorder: recorder,
	}
	targetRotations := []certrotation.TargetRotation{
		{
			Namespace:     helpers.ClusterManagerNamespace,
			Name:          helpers.RegistrationWebhookSecret,
			Validity:      TargetCertValidity,
			HostNames:     []string{fmt.Sprintf("%s.%s.svc", helpers.RegistrationWebhookService, helpers.ClusterManagerNamespace)},
			Lister:        secretInformer.Lister(),
			Client:        kubeClient.CoreV1(),
			EventRecorder: recorder,
		},
		{
			Namespace:     helpers.ClusterManagerNamespace,
			Name:          helpers.WorkWebhookSecret,
			Validity:      TargetCertValidity,
			HostNames:     []string{fmt.Sprintf("%s.%s.svc", helpers.WorkWebhookService, helpers.ClusterManagerNamespace)},
			Lister:        secretInformer.Lister(),
			Client:        kubeClient.CoreV1(),
			EventRecorder: recorder,
		},
	}

	c := &certRotationController{
		signingRotation:      signingRotation,
		caBundleRotation:     caBundleRotation,
		targetRotations:      targetRotations,
		kubeClient:           kubeClient,
		clusterManagerLister: clusterManagerInformer.Lister(),
	}
	return factory.New().
		ResyncEvery(ResyncInterval).
		WithSync(c.sync).
		WithInformers(
			secretInformer.Informer(),
			configMapInformer.Informer(),
			clusterManagerInformer.Informer(),
		).
		ToController("CertRotationController", recorder)
}

func (c certRotationController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// do nothing if there is no cluster manager
	clustermanagers, err := c.clusterManagerLister.List(labels.Everything())
	if err != nil {
		return err
	}
	if len(clustermanagers) == 0 {
		klog.V(4).Infof("No ClusterManager found")
		return nil
	}

	klog.Infof("Reconciling ClusterManager %q", clustermanagers[0].Name)
	// do nothing if the cluster manager is deleting
	if !clustermanagers[0].DeletionTimestamp.IsZero() {
		return nil
	}

	// check if namespace exists or not
	_, err = c.kubeClient.CoreV1().Namespaces().Get(ctx, helpers.ClusterManagerNamespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return fmt.Errorf("namespace %q does not exist yet", helpers.ClusterManagerNamespace)
	}
	if err != nil {
		return err
	}

	// reconcile cert/key pair for signer
	signingCertKeyPair, err := c.signingRotation.EnsureSigningCertKeyPair()
	if err != nil {
		return err
	}

	// reconcile ca bundle
	cabundleCerts, err := c.caBundleRotation.EnsureConfigMapCABundle(signingCertKeyPair)
	if err != nil {
		return err
	}

	// reconcile target cert/key pairs
	errs := []error{}
	for _, targetRotation := range c.targetRotations {
		if err := targetRotation.EnsureTargetCertKeyPair(signingCertKeyPair, cabundleCerts); err != nil {
			errs = append(errs, err)
		}
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}
