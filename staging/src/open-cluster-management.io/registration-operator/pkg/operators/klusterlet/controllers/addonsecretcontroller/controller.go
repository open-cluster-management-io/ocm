package addonsecretcontroller

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

const (
	imagePullSecret               = "open-cluster-management-image-pull-credentials"
	addonInstallNamespaceLabelKey = "addon.open-cluster-management.io/namespace"
)

// AddonPullImageSecretController is used to sync pull image secret from operator namespace to addon namespaces(with label "addon.open-cluster-management.io/namespace":"true")
// Note:
// 1. AddonPullImageSecretController only handles namespace events within the same cluster.
// 2. If the lable is remove from namespace, controller now would not remove the secret.
type addonPullImageSecretController struct {
	operatorNamespace string
	namespaceInformer coreinformer.NamespaceInformer
	kubeClient        kubernetes.Interface
	recorder          events.Recorder
}

func NewAddonPullImageSecretController(kubeClient kubernetes.Interface, operatorNamespace string, namespaceInformer coreinformer.NamespaceInformer, recorder events.Recorder) factory.Controller {
	ac := &addonPullImageSecretController{
		operatorNamespace: operatorNamespace,
		namespaceInformer: namespaceInformer,
		kubeClient:        kubeClient,
		recorder:          recorder,
	}
	return factory.New().WithFilteredEventsInformersQueueKeyFunc(func(o runtime.Object) string {
		namespace := o.(*corev1.Namespace)
		return namespace.GetName()
	}, func(obj interface{}) bool {
		// if obj has the label, return true
		namespace := obj.(*corev1.Namespace)
		if namespace.Labels[addonInstallNamespaceLabelKey] == "true" {
			return true
		}
		return false
	}, namespaceInformer.Informer()).WithSync(ac.sync).ToController("AddonPullImageSecretController", recorder)
}

func (c *addonPullImageSecretController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	var err error

	// Sync secret if namespace is created
	namespace := controllerContext.QueueKey()
	if namespace == "" {
		return nil
	}

	// If namespace does't have addon label, do nothing
	ns, err := c.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if ns.Labels[addonInstallNamespaceLabelKey] != "true" {
		return nil
	}

	_, _, err = helpers.SyncSecret(
		ctx,
		c.kubeClient.CoreV1(),
		c.kubeClient.CoreV1(),
		c.recorder,
		c.operatorNamespace,
		imagePullSecret,
		namespace,
		imagePullSecret,
		[]metav1.OwnerReference{},
	)
	if err != nil {
		return err
	}
	return nil
}
