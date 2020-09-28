package bootstrapcontroller

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	operatorinformer "github.com/open-cluster-management/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "github.com/open-cluster-management/api/client/operator/listers/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"

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
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
)

// bootstrapController watches bootstrap hub kubeconfig secret, if the secret is changed with hub kube-apiserver ca or apiserver
// endpoints, this controller will make the klusterlet re-bootstrap to get the new hub kubeconfig from hub cluster by deleting
// the current hub kubeconfig secret and restart the klusterlet agents
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
		ToController("BootstrapController", recorder)
}

func (k *bootstrapController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	queueKey := controllerContext.QueueKey()
	if queueKey == "" {
		return nil
	}

	keys := strings.Split(queueKey, "/")
	if len(keys) != 2 {
		// this should not happen, do nothing
		return nil
	}
	klusterletNamespace := keys[0]
	klusterletName := keys[1]

	klog.V(4).Infof("Reconciling bootstrap hub kubeconfig secret %q", klusterletNamespace+"/"+helpers.BootstrapHubKubeConfig)

	bootstrapHubKubeconfigSecret, err := k.secretLister.Secrets(klusterletNamespace).Get(helpers.BootstrapHubKubeConfig)
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
			fmt.Sprintf("unable to load hub kubeconfig from secret %q: %v", klusterletNamespace+"/"+helpers.BootstrapHubKubeConfig, err))
		return nil
	}

	hubKubeconfigSecret, err := k.secretLister.Secrets(klusterletNamespace).Get(helpers.HubKubeConfig)
	switch {
	case errors.IsNotFound(err):
		// the hub kubeconfig secret not found, could not have bootstrap yet, do nothing
		return nil
	case err != nil:
		return err
	}

	hubKubeconfig, err := k.loadKubeConfig(hubKubeconfigSecret)
	if err != nil {
		// the hub kubeconfig secret has errors, do nothing
		controllerContext.Recorder().Warningf("BadHubKubeConfigSecret",
			fmt.Sprintf("unable to load hub kubeconfig from secret %q: %v", klusterletNamespace+"/"+helpers.BootstrapHubKubeConfig, err))
		return nil
	}

	// the CA and server are not changed in bootstrap kubeconfig secret, ignore this change
	if bootstrapKubeconfig.Server == hubKubeconfig.Server &&
		bytes.Equal(bootstrapKubeconfig.CertificateAuthorityData, hubKubeconfig.CertificateAuthorityData) {
		return nil
	}

	// the bootstrap kubeconfig secret is changed, reload the klusterlet agents
	return k.reloadAgents(ctx, controllerContext, klusterletNamespace, klusterletName)
}

// reloadAgents reload klusterlet agents by
// 1. make the registration agent re-bootstrap by deleting the current hub kubeconfig secret to
// 2. restart the registration and work agents to reload the new hub ca by deleting the agent deployments
func (k *bootstrapController) reloadAgents(ctx context.Context, ctrlContext factory.SyncContext, namespace, klusterletName string) error {
	if err := k.kubeClient.CoreV1().Secrets(namespace).Delete(ctx, helpers.HubKubeConfig, metav1.DeleteOptions{}); err != nil {
		return err
	}
	ctrlContext.Recorder().Eventf("HubKubeconfigSecretDeleted",
		fmt.Sprintf("the hub kubeconfig secret %q is deleted due to the bootstrap secret %q is changed",
			namespace+"/"+helpers.HubKubeConfig, namespace+"/"+helpers.BootstrapHubKubeConfig))

	registrationName := fmt.Sprintf("%s-registration-agent", klusterletName)
	if err := k.kubeClient.AppsV1().Deployments(namespace).Delete(ctx, registrationName, metav1.DeleteOptions{}); err != nil {
		return err
	}
	ctrlContext.Recorder().Eventf("KlusterletAgentDeploymentDeleted",
		fmt.Sprintf("the deployment %q is deleted due to the bootstrap secret %q is changed",
			namespace+"/"+registrationName, namespace+"/"+helpers.BootstrapHubKubeConfig))

	workName := fmt.Sprintf("%s-work-agent", klusterletName)
	if err := k.kubeClient.AppsV1().Deployments(namespace).Delete(ctx, workName, metav1.DeleteOptions{}); err != nil {
		return err
	}
	ctrlContext.Recorder().Eventf("KlusterletAgentDeploymentDeleted",
		fmt.Sprintf("the deployment %q is deleted due to the bootstrap secret %q is changed",
			namespace+"/"+workName, namespace+"/"+helpers.BootstrapHubKubeConfig))

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
