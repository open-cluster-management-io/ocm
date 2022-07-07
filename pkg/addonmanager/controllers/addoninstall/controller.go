package addoninstall

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
)

// managedClusterController reconciles instances of ManagedCluster on the hub.
type addonInstallController struct {
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	agentAddons               map[string]agent.AgentAddon
}

func NewAddonInstallController(
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
) factory.Controller {
	c := &addonInstallController{
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		agentAddons:               agentAddons,
	}

	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			accessor, _ := meta.Accessor(obj)
			return []string{accessor.GetNamespace()}
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if _, ok := c.agentAddons[accessor.GetName()]; !ok {
				return false
			}

			return true
		},
		addonInformers.Informer()).
		WithInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				accessor, _ := meta.Accessor(obj)
				return []string{accessor.GetName()}
			},
			clusterInformers.Informer(),
		).
		WithSync(c.sync).ToController("addon-install-controller")
}

func (c *addonInstallController) sync(ctx context.Context, syncCtx factory.SyncContext, clusterName string) error {
	klog.V(4).Infof("Reconciling addon deploy on cluster %q", clusterName)

	cluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// if cluster is deleting, do not install addon
	if !cluster.DeletionTimestamp.IsZero() {
		klog.V(4).Infof("Cluster %q is deleting, skip addon deploy", clusterName)
		return nil
	}

	for addonName, addon := range c.agentAddons {
		if addon.GetAgentAddonOptions().InstallStrategy == nil {
			continue
		}

		switch addon.GetAgentAddonOptions().InstallStrategy.Type {
		case agent.InstallAll:
			return c.applyAddon(ctx, addonName, clusterName, addon.GetAgentAddonOptions().InstallStrategy.InstallNamespace)
		case agent.InstallByLabel:
			labelSelector := addon.GetAgentAddonOptions().InstallStrategy.LabelSelector
			if labelSelector == nil {
				klog.Warningf("installByLabel strategy is set, but label selector is not set")
				return nil
			}

			selector, err := metav1.LabelSelectorAsSelector(labelSelector)
			if err != nil {
				klog.Warningf("labels selector is not correct: %v", err)
				return nil
			}

			if !selector.Matches(labels.Set(cluster.Labels)) {
				return nil
			}

			return c.applyAddon(ctx, addonName, clusterName, addon.GetAgentAddonOptions().InstallStrategy.InstallNamespace)
		}
	}

	return nil
}

func (c *addonInstallController) applyAddon(ctx context.Context, addonName, clusterName, installNamespace string) error {
	_, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)

	// only create addon when it is missing, if user update the addon resource ,it should not be reverted
	if errors.IsNotFound(err) {
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addonName,
				Namespace: clusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: installNamespace,
			},
		}
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Create(ctx, addon, metav1.CreateOptions{})
		return err
	}

	return err
}
