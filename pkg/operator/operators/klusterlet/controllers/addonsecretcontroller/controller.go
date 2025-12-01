package addonsecretcontroller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const (
	addonInstallNamespaceLabelKey = "addon.open-cluster-management.io/namespace"
)

// AddonPullImageSecretController is used to sync pull image secret from operator namespace
// to addon namespaces(with label "addon.open-cluster-management.io/namespace":"true")
// Note:
// 1. AddonPullImageSecretController only handles namespace events within the same cluster.
// 2. If the lable is remove from namespace, controller now would not remove the secret.
type addonPullImageSecretController struct {
	operatorNamespace string
	namespaceInformer coreinformer.NamespaceInformer
	kubeClient        kubernetes.Interface
}

func NewAddonPullImageSecretController(kubeClient kubernetes.Interface, operatorNamespace string,
	namespaceInformer coreinformer.NamespaceInformer) factory.Controller {
	ac := &addonPullImageSecretController{
		operatorNamespace: operatorNamespace,
		namespaceInformer: namespaceInformer,
		kubeClient:        kubeClient,
	}
	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		queue.QueueKeyByMetaName,
		queue.FileterByLabelKeyValue(addonInstallNamespaceLabelKey, "true"),
		namespaceInformer.Informer()).WithSync(ac.sync).ToController("AddonPullImageSecretController")
}

func (c *addonPullImageSecretController) sync(ctx context.Context, syncCtx factory.SyncContext, namespace string) error {
	var err error

	// Sync secret if namespace is created
	if namespace == "" {
		return nil
	}

	// If namespace is not found or deleting or does't have addon label, do nothing
	ns, err := c.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ns.DeletionTimestamp.IsZero() {
		return nil
	}

	if ns.Labels[addonInstallNamespaceLabelKey] != "true" {
		return nil
	}

	_, _, err = helpers.SyncSecret(
		ctx,
		c.kubeClient.CoreV1(),
		c.kubeClient.CoreV1(),
		syncCtx.Recorder(),
		c.operatorNamespace,
		helpers.ImagePullSecret,
		namespace,
		helpers.ImagePullSecret,
		[]metav1.OwnerReference{},
		nil,
	)
	if err != nil {
		return err
	}
	return nil
}
