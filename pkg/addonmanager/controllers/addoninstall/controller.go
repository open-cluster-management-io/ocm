package addoninstall

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
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

	if value, ok := cluster.Annotations[addonapiv1alpha1.DisableAddonAutomaticInstallationAnnotationKey]; ok &&
		strings.EqualFold(value, "true") {

		klog.V(4).Infof("Cluster %q has annotation %q, skip addon deploy",
			clusterName, addonapiv1alpha1.DisableAddonAutomaticInstallationAnnotationKey)
		return nil
	}

	var errs []error

	for addonName, addon := range c.agentAddons {
		if addon.GetAgentAddonOptions().InstallStrategy == nil {
			continue
		}

		managedClusterFilter := addon.GetAgentAddonOptions().InstallStrategy.GetManagedClusterFilter()
		if managedClusterFilter == nil {
			continue
		}
		if !managedClusterFilter(cluster) {
			klog.V(4).Infof("managed cluster filter is not match for addon %s on %s", addonName, clusterName)
			continue
		}

		err = c.applyAddon(ctx, addonName, clusterName, addon.GetAgentAddonOptions().InstallStrategy.InstallNamespace)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errorsutil.NewAggregate(errs)
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
