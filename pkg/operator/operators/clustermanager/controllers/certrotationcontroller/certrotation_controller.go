package certrotationcontroller

import (
	"context"
	"fmt"
	"slices"
	"time"

	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/certrotation"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const (
	signerNamePrefix = "cluster-manager-webhook"
)

// Follow the rules below to set the value of SigningCertValidity/TargetCertValidity/ResyncInterval:
//
// 1) SigningCertValidity * 1/5 * 1/5 > ResyncInterval * 2
// 2) TargetCertValidity * 1/5 > ResyncInterval * 2
var SigningCertValidity = time.Hour * 24 * 365
var TargetCertValidity = time.Hour * 24 * 30
var ResyncInterval = time.Minute * 10

// certRotationController does:
//
//  1. continuously create a self-signed signing CA (via SigningRotation).
//     It creates the next one when a given percentage of the validity of the old CA has passed.
//  2. maintain a CA bundle with all not yet expired CA certs.
//  3. continuously create target cert/key pairs signed by the latest signing CA
//     It creates the next one when a given percentage of the validity of the previous cert has
//     passed, or when a new CA has been created.
type certRotationController struct {
	rotationMap          map[string]rotations // key is clusterManager's name, value is a rotations struct
	kubeClient           kubernetes.Interface
	secretInformers      map[string]corev1informers.SecretInformer
	configMapInformer    corev1informers.ConfigMapInformer
	clusterManagerLister operatorlister.ClusterManagerLister
}

type rotations struct {
	signingRotation  certrotation.SigningRotation
	caBundleRotation certrotation.CABundleRotation
	targetRotations  map[string]certrotation.TargetRotation
}

func NewCertRotationController(
	kubeClient kubernetes.Interface,
	secretInformers map[string]corev1informers.SecretInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
) factory.Controller {
	c := &certRotationController{
		rotationMap:          make(map[string]rotations),
		kubeClient:           kubeClient,
		secretInformers:      secretInformers,
		configMapInformer:    configMapInformer,
		clusterManagerLister: clusterManagerInformer.Lister(),
	}
	return factory.New().
		ResyncEvery(ResyncInterval).
		WithSync(c.sync).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterManagerInformer.Informer()).
		WithInformersQueueKeysFunc(helpers.ClusterManagerQueueKeyFunc(c.clusterManagerLister),
			configMapInformer.Informer(),
			secretInformers[helpers.SignerSecret].Informer(),
			secretInformers[helpers.RegistrationWebhookSecret].Informer(),
			secretInformers[helpers.WorkWebhookSecret].Informer(),
			secretInformers[helpers.GRPCServerSecret].Informer()).
		ToController("CertRotationController")
}

func (c certRotationController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	logger := klog.FromContext(ctx).WithValues("key", key)
	switch {
	case key == "":
		return nil
	case key == factory.DefaultQueueKey:
		// ensure every clustermanager's certificates
		clustermanagers, err := c.clusterManagerLister.List(labels.Everything())
		if err != nil {
			return err
		}

		// do nothing if there is no cluster manager
		if len(clustermanagers) == 0 {
			logger.V(4).Info("No ClusterManager found")
			return nil
		}

		var errs []error
		for i := range clustermanagers {
			err = c.syncOne(ctx, clustermanagers[i])
			if err != nil {
				errs = append(errs, err)
			}
		}
		return operatorhelpers.NewMultiLineAggregate(errs)
	default:
		clustermanagerName := key
		clustermanager, err := c.clusterManagerLister.Get(clustermanagerName)
		// ClusterManager not found, could have been deleted, do nothing.
		if errors.IsNotFound(err) {
			logger.V(4).Info("ClusterManager not found; it may have been deleted",
				"clustermanager", clustermanagerName)
			return nil
		}
		err = c.syncOne(ctx, clustermanager)
		if err != nil {
			return err
		}
		return nil
	}
}

func (c certRotationController) syncOne(ctx context.Context, clustermanager *operatorv1.ClusterManager) error {
	clustermanagerName := clustermanager.Name
	clustermanagerNamespace := helpers.ClusterManagerNamespace(clustermanager.Name, clustermanager.Spec.DeployOption.Mode)

	logger := klog.FromContext(ctx).WithValues("clustermanager", clustermanagerName)

	var err error

	logger.Info("Reconciling ClusterManager")
	// if the cluster manager is deleting, delete the rotation in map as well.
	if !clustermanager.DeletionTimestamp.IsZero() {
		// clean up all resources related with this clustermanager
		if _, ok := c.rotationMap[clustermanagerName]; ok {
			// delete signerSecret
			err = c.kubeClient.CoreV1().Secrets(clustermanagerNamespace).Delete(ctx, helpers.SignerSecret, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("clean up deleted cluster-manager, deleting signer secret failed, err:%s", err.Error())
			}

			// delete caBundleConfig
			err = c.kubeClient.CoreV1().ConfigMaps(clustermanagerNamespace).Delete(ctx, helpers.CaBundleConfigmap, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("clean up deleted cluster-manager, deleting caBundle config failed, err:%s", err.Error())
			}

			// delete registration webhook secret
			err = c.kubeClient.CoreV1().Secrets(clustermanagerNamespace).Delete(ctx, helpers.RegistrationWebhookSecret, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("clean up deleted cluster-manager, deleting registration webhook secret failed, err:%s", err.Error())
			}

			// delete work webhook secret
			err = c.kubeClient.CoreV1().Secrets(clustermanagerNamespace).Delete(ctx, helpers.WorkWebhookSecret, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("clean up deleted cluster-manager, deleting work webhook secret failed, err:%s", err.Error())
			}

			// delete grpc server secret
			err = c.kubeClient.CoreV1().Secrets(clustermanagerNamespace).Delete(ctx, helpers.GRPCServerSecret, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("clean up deleted cluster-manager, deleting grpc server secret failed, err:%s", err.Error())
			}

			delete(c.rotationMap, clustermanagerName)
		}
		return nil
	}

	_, err = c.kubeClient.CoreV1().Namespaces().Get(ctx, clustermanagerNamespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return fmt.Errorf("namespace %q does not exist yet", clustermanagerNamespace)
	}
	if err != nil {
		return err
	}

	// delete the grpc serving secret if the grpc auth is disabled
	if !helpers.GRPCAuthEnabled(clustermanager) {
		if _, ok := c.rotationMap[clustermanager.Name]; ok {
			delete(c.rotationMap[clustermanager.Name].targetRotations, helpers.GRPCServerSecret)
		}

		err = c.kubeClient.CoreV1().Secrets(clustermanagerNamespace).Delete(ctx, helpers.GRPCServerSecret, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("clean up deleted cluster-manager, deleting grpc server secret failed, err:%s", err.Error())
		}
	}

	// check if rotations exist, if not exist then create one
	if _, ok := c.rotationMap[clustermanager.Name]; !ok {
		signingRotation := certrotation.SigningRotation{
			Namespace:        clustermanagerNamespace,
			Name:             helpers.SignerSecret,
			SignerNamePrefix: signerNamePrefix,
			Validity:         SigningCertValidity,
			Lister:           c.secretInformers[helpers.SignerSecret].Lister(),
			Client:           c.kubeClient.CoreV1(),
		}
		caBundleRotation := certrotation.CABundleRotation{
			Namespace: clustermanagerNamespace,
			Name:      helpers.CaBundleConfigmap,
			Lister:    c.configMapInformer.Lister(),
			Client:    c.kubeClient.CoreV1(),
		}
		targetRotations := map[string]certrotation.TargetRotation{
			helpers.RegistrationWebhookSecret: {
				Namespace: clustermanagerNamespace,
				Name:      helpers.RegistrationWebhookSecret,
				Validity:  TargetCertValidity,
				HostNames: []string{fmt.Sprintf("%s.%s.svc", helpers.RegistrationWebhookService, clustermanagerNamespace)},
				Lister:    c.secretInformers[helpers.RegistrationWebhookSecret].Lister(),
				Client:    c.kubeClient.CoreV1(),
			},
			helpers.WorkWebhookSecret: {
				Namespace: clustermanagerNamespace,
				Name:      helpers.WorkWebhookSecret,
				Validity:  TargetCertValidity,
				HostNames: []string{fmt.Sprintf("%s.%s.svc", helpers.WorkWebhookService, clustermanagerNamespace)},
				Lister:    c.secretInformers[helpers.WorkWebhookSecret].Lister(),
				Client:    c.kubeClient.CoreV1(),
			},
		}

		c.rotationMap[clustermanagerName] = rotations{
			signingRotation:  signingRotation,
			caBundleRotation: caBundleRotation,
			targetRotations:  targetRotations,
		}
	}

	var errs []error
	// Ensure certificates are exists
	cmRotations := c.rotationMap[clustermanagerName]

	if helpers.GRPCAuthEnabled(clustermanager) {
		// maintain the grpc serving certs
		// TODO may support user provided certs
		hostNames, grpcErr := helpers.GRPCServerHostNames(c.kubeClient, clustermanagerNamespace, clustermanager)
		if grpcErr != nil {
			errs = append(errs, grpcErr)
		} else if targetRotation, ok := cmRotations.targetRotations[helpers.GRPCServerSecret]; ok {
			if !slices.Equal(targetRotation.HostNames, hostNames) {
				targetRotation.HostNames = hostNames
				cmRotations.targetRotations[helpers.GRPCServerSecret] = targetRotation
				logger.Info("the hosts of grpc server are changed, will update the grpc serving cert")
			}

		} else {
			c.rotationMap[clustermanagerName].targetRotations[helpers.GRPCServerSecret] = certrotation.TargetRotation{
				Namespace: clustermanagerNamespace,
				Name:      helpers.GRPCServerSecret,
				Validity:  TargetCertValidity,
				HostNames: hostNames,
				Lister:    c.secretInformers[helpers.GRPCServerSecret].Lister(),
				Client:    c.kubeClient.CoreV1(),
			}
		}
	}

	// reconcile cert/key pair for signer
	signingCertKeyPair, err := cmRotations.signingRotation.EnsureSigningCertKeyPair()
	if err != nil {
		return err
	}

	// reconcile ca bundle
	cabundleCerts, err := cmRotations.caBundleRotation.EnsureConfigMapCABundle(signingCertKeyPair)
	if err != nil {
		return err
	}

	// reconcile target cert/key pairs
	for _, targetRotation := range cmRotations.targetRotations {
		if err := targetRotation.EnsureTargetCertKeyPair(signingCertKeyPair, cabundleCerts); err != nil {
			errs = append(errs, err)
		}
	}

	return errorhelpers.NewMultiLineAggregate(errs)
}
