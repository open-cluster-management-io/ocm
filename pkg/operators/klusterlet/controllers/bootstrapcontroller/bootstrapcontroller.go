package bootstrapcontroller

import (
	"bytes"
	"context"
	"fmt"
	"time"

	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2"
)

const tlsCertFile = "tls.crt"

// BootstrapControllerSyncInterval is exposed so that integration tests can crank up the constroller sync speed.
var BootstrapControllerSyncInterval = 5 * time.Minute

// bootstrapController watches bootstrap-hub-kubeconfig and hub-kubeconfig-secret secrets, if the bootstrap-hub-kubeconfig secret
// is changed with hub kube-apiserver ca or apiserver endpoints, or the hub-kubeconfig-secret secret is expired, this controller
// will make the klusterlet re-bootstrap to get the new hub kubeconfig from hub cluster by deleting the current hub kubeconfig
// secret and restart the klusterlet agents
type bootstrapController struct {
	kubeClient       kubernetes.Interface
	klusterletLister operatorlister.KlusterletLister
	secretLister     corelister.SecretLister
}

// NewBootstrapController returns a bootstrapController
func NewBootstrapController(
	kubeClient kubernetes.Interface,
	klusterletInformer operatorinformer.KlusterletInformer,
	secretInformer coreinformer.SecretInformer,
	recorder events.Recorder) factory.Controller {
	controller := &bootstrapController{
		kubeClient:       kubeClient,
		klusterletLister: klusterletInformer.Lister(),
		secretLister:     secretInformer.Lister(),
	}
	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(bootstrapSecretQueueKeyFunc(controller.klusterletLister), secretInformer.Informer()).
		ResyncEvery(BootstrapControllerSyncInterval).
		ToController("BootstrapController", recorder)
}

func (k *bootstrapController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	queueKey := controllerContext.QueueKey()
	if queueKey == "" {
		return nil
	}

	klog.V(4).Infof("Reconciling klusterlet kubeconfig secrets %q", queueKey)

	agentNamespace, klusterletName, err := cache.SplitMetaNamespaceKey(queueKey)
	if err != nil {
		// ignore bad format key
		return nil
	}

	// triggered by resync, checking whether the hub kubeconfig secret is expired
	if agentNamespace == "" && klusterletName == factory.DefaultQueueKey {
		klusterlets, err := k.klusterletLister.List(labels.Everything())
		if err != nil {
			return err
		}

		for _, klusterlet := range klusterlets {
			namespace := helpers.AgentNamespace(klusterlet)
			// enqueue the klusterlet to reconcile
			controllerContext.Queue().Add(fmt.Sprintf("%s/%s", namespace, klusterlet.Name))
		}

		return nil
	}

	bootstrapHubKubeconfigSecret, err := k.secretLister.Secrets(agentNamespace).Get(helpers.BootstrapHubKubeConfig)
	switch {
	case errors.IsNotFound(err):
		// the bootstrap hub kubeconfig secret not found, do nothing
		return nil
	case err != nil:
		return err
	}

	bootstrapKubeconfig, err := k.loadKubeConfig(bootstrapHubKubeconfigSecret)
	if err != nil {
		// a bad bootstrap secret, ignore it
		controllerContext.Recorder().Warningf("BadBootstrapSecret",
			fmt.Sprintf("unable to load hub kubeconfig from secret %s/%s: %v", agentNamespace, helpers.BootstrapHubKubeConfig, err))
		return nil
	}

	hubKubeconfigSecret, err := k.secretLister.Secrets(agentNamespace).Get(helpers.HubKubeConfig)
	switch {
	case errors.IsNotFound(err):
		// the hub kubeconfig secret not found, could not have bootstrap yet, do nothing currently
		// TODO one case should be supported in the future: the bootstrap phase may be failed due to
		// the content of bootstrap secret is wrong, this also results in the hub kubeconfig secret
		// cannot be found. In this case, user may need to correct the bootstrap secret. we need to
		// find a way to know the bootstrap secret is corrected, and then reload the klusterlet
		return nil
	case err != nil:
		return err
	}

	hubKubeconfig, err := k.loadKubeConfig(hubKubeconfigSecret)
	if err != nil {
		// the hub kubeconfig secret has errors, do nothing
		controllerContext.Recorder().Warningf("BadHubKubeConfigSecret",
			fmt.Sprintf("unable to load hub kubeconfig from secret %s/%s: %v", agentNamespace, helpers.BootstrapHubKubeConfig, err))
		return nil
	}

	if bootstrapKubeconfig.Server != hubKubeconfig.Server ||
		!bytes.Equal(bootstrapKubeconfig.CertificateAuthorityData, hubKubeconfig.CertificateAuthorityData) {
		// the bootstrap kubeconfig secret is changed, reload the klusterlet agents
		reloadReason := fmt.Sprintf("the bootstrap secret %s/%s is changed", agentNamespace, helpers.BootstrapHubKubeConfig)
		return k.reloadAgents(ctx, controllerContext, agentNamespace, klusterletName, reloadReason)
	}

	expired, err := isHubKubeconfigSecretExpired(hubKubeconfigSecret)
	if err != nil {
		// the hub kubeconfig secret has errors, do nothing
		controllerContext.Recorder().Warningf("BadHubKubeConfigSecret",
			fmt.Sprintf("the hub kubeconfig secret %s/%s is invalid: %v", agentNamespace, helpers.HubKubeConfig, err))
		return nil
	}

	// the hub kubeconfig secret cert is not expired, do nothing
	if !expired {
		return nil
	}

	// the hub kubeconfig secret cert is expired, reload klusterlet to restart bootstrap
	reloadReason := fmt.Sprintf("the hub kubeconfig secret %s/%s is expired", agentNamespace, helpers.HubKubeConfig)
	return k.reloadAgents(ctx, controllerContext, agentNamespace, klusterletName, reloadReason)
}

// reloadAgents reload klusterlet agents by
// 1. make the registration agent re-bootstrap by deleting the current hub kubeconfig secret to
// 2. restart the registration and work agents to reload the new hub ca by deleting the agent deployments
func (k *bootstrapController) reloadAgents(ctx context.Context, ctrlContext factory.SyncContext, namespace, klusterletName, reason string) error {
	if err := k.kubeClient.CoreV1().Secrets(namespace).Delete(ctx, helpers.HubKubeConfig, metav1.DeleteOptions{}); err != nil {
		return err
	}
	ctrlContext.Recorder().Eventf("HubKubeconfigSecretDeleted", fmt.Sprintf("the hub kubeconfig secret %s/%s is deleted due to %s",
		namespace, helpers.HubKubeConfig, reason))

	registrationName := fmt.Sprintf("%s-registration-agent", klusterletName)
	if err := k.kubeClient.AppsV1().Deployments(namespace).Delete(ctx, registrationName, metav1.DeleteOptions{}); err != nil {
		return err
	}
	ctrlContext.Recorder().Eventf("KlusterletAgentDeploymentDeleted", fmt.Sprintf("the deployment %s/%s is deleted due to %s",
		namespace, registrationName, reason))

	workName := fmt.Sprintf("%s-work-agent", klusterletName)
	if err := k.kubeClient.AppsV1().Deployments(namespace).Delete(ctx, workName, metav1.DeleteOptions{}); err != nil {
		return err
	}
	ctrlContext.Recorder().Eventf("KlusterletAgentDeploymentDeleted", fmt.Sprintf("the deployment %s/%s is deleted due to %s",
		namespace, workName, reason))

	return nil
}

func (k *bootstrapController) loadKubeConfig(secret *corev1.Secret) (*clientcmdapi.Cluster, error) {
	kubeconfig, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("unable to get kubeconfig in secret")
	}
	config, err := clientcmd.Load(kubeconfig)
	if err != nil {
		return nil, err
	}
	currentContext, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return nil, fmt.Errorf("unable to get current-context in kubeconfig")
	}
	cluster, ok := config.Clusters[currentContext.Cluster]
	if !ok {
		return nil, fmt.Errorf("unable to get current cluster %q in kubeconfig", currentContext.Cluster)
	}
	return cluster, nil
}

func bootstrapSecretQueueKeyFunc(klusterletLister operatorlister.KlusterletLister) factory.ObjectQueueKeyFunc {
	return func(obj runtime.Object) string {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return ""
		}
		name := accessor.GetName()
		if name != helpers.BootstrapHubKubeConfig {
			return ""
		}

		namespace := accessor.GetNamespace()
		klusterlets, err := klusterletLister.List(labels.Everything())
		if err != nil {
			return ""
		}

		if klusterlet := helpers.FindKlusterletByNamespace(klusterlets, namespace); klusterlet != nil {
			return namespace + "/" + klusterlet.Name
		}

		return ""
	}
}

func isHubKubeconfigSecretExpired(secret *corev1.Secret) (bool, error) {
	certData, ok := secret.Data[tlsCertFile]
	if !ok {
		return false, fmt.Errorf("there is no %q", tlsCertFile)
	}

	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return false, fmt.Errorf("failed to parse cert: %v", err)
	}

	if len(certs) == 0 {
		return false, fmt.Errorf("there are no certs in %q", tlsCertFile)
	}

	now := time.Now()
	for _, cert := range certs {
		if now.After(cert.NotAfter) {
			return true, nil
		}
	}

	return false, nil
}
