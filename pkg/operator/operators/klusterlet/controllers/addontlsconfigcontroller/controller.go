package addontlsconfigcontroller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	tlslib "open-cluster-management.io/sdk-go/pkg/tls"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	addonInstallNamespaceLabelKey = "addon.open-cluster-management.io/namespace"
)

// addonTLSConfigController copies the ocm-tls-profile ConfigMap from the operator namespace
// to addon namespaces (labeled with "addon.open-cluster-management.io/namespace":"true").
// This allows addon agents to read TLS profile settings without needing cross-namespace access.
type addonTLSConfigController struct {
	operatorNamespace string
	kubeClient        kubernetes.Interface
}

func NewAddonTLSConfigController(kubeClient kubernetes.Interface, operatorNamespace string,
	namespaceInformer coreinformer.NamespaceInformer) factory.Controller {
	c := &addonTLSConfigController{
		operatorNamespace: operatorNamespace,
		kubeClient:        kubeClient,
	}
	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		queue.QueueKeyByMetaName,
		queue.FileterByLabelKeyValue(addonInstallNamespaceLabelKey, "true"),
		namespaceInformer.Informer()).WithSync(c.sync).ToController("AddonTLSConfigController")
}

func (c *addonTLSConfigController) sync(ctx context.Context, _ factory.SyncContext, namespace string) error {
	if namespace == "" {
		return nil
	}

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

	return c.syncConfigMap(ctx, namespace)
}

func (c *addonTLSConfigController) syncConfigMap(ctx context.Context, targetNamespace string) error {
	name := tlslib.ConfigMapName

	source, err := c.kubeClient.CoreV1().ConfigMaps(c.operatorNamespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Source doesn't exist — clean up target if it exists
		if delErr := c.kubeClient.CoreV1().ConfigMaps(targetNamespace).Delete(
			ctx, name, metav1.DeleteOptions{}); delErr != nil && !errors.IsNotFound(delErr) {
			return delErr
		}
		return nil
	}
	if err != nil {
		return err
	}

	existing, err := c.kubeClient.CoreV1().ConfigMaps(targetNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err == nil {
		// Target exists — update only if data changed
		if equality.Semantic.DeepEqual(existing.Data, source.Data) {
			return nil
		}
		existing.Data = source.Data
		_, err = c.kubeClient.CoreV1().ConfigMaps(targetNamespace).Update(ctx, existing, metav1.UpdateOptions{})
	} else {
		// Target doesn't exist — create it
		target := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: targetNamespace,
			},
			Data: source.Data,
		}
		_, err = c.kubeClient.CoreV1().ConfigMaps(targetNamespace).Create(ctx, target, metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}

	klog.Infof("Synced ConfigMap %s from %s to %s", name, c.operatorNamespace, targetNamespace)
	return nil
}
