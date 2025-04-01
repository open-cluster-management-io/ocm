package addon

import (
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1alpha1"
)

// ClusterClientSetWrapper wraps a cluster client that has a ManagedCluster client to a cluster
// clientset interface, this wrapper will helps us to build ManagedCluster informer factory easily.
type AddonClientSetWrapper struct {
	AddonV1Alpha1ClientWrapper *AddonV1Alpha1ClientWrapper
}

func (a AddonClientSetWrapper) Discovery() discovery.DiscoveryInterface {
	//TODO implement me
	panic("implement me")
}

func (a AddonClientSetWrapper) AddonV1alpha1() addonv1alpha1client.AddonV1alpha1Interface {
	//TODO implement me
	return a.AddonV1Alpha1ClientWrapper
}

var _ addonclientset.Interface = &AddonClientSetWrapper{}

// ClusterV1ClientWrapper wraps a ManagedCluster client to a ClusterV1Interface
type AddonV1Alpha1ClientWrapper struct {
	ManagedClusterAddonClient addonv1alpha1client.ManagedClusterAddOnInterface
	namespace                 string
}

func (c *AddonV1Alpha1ClientWrapper) AddOnDeploymentConfigs(namespace string) addonv1alpha1client.AddOnDeploymentConfigInterface {
	//TODO implement me
	panic("implement me")
}

func (c *AddonV1Alpha1ClientWrapper) AddOnTemplates() addonv1alpha1client.AddOnTemplateInterface {
	//TODO implement me
	panic("implement me")
}

func (c *AddonV1Alpha1ClientWrapper) ClusterManagementAddOns() addonv1alpha1client.ClusterManagementAddOnInterface {
	//TODO implement me
	panic("implement me")
}

func (c *AddonV1Alpha1ClientWrapper) ManagedClusterAddOns(namespace string) addonv1alpha1client.ManagedClusterAddOnInterface {
	if agentManagedClusterClient, ok := c.ManagedClusterAddonClient.(*ManagedClusterAddOnClient); ok {
		return agentManagedClusterClient.Namespace(namespace)
	}

	return nil
}

var _ addonv1alpha1client.AddonV1alpha1Interface = &AddonV1Alpha1ClientWrapper{}

func (c *AddonV1Alpha1ClientWrapper) RESTClient() rest.Interface {
	return nil
}
