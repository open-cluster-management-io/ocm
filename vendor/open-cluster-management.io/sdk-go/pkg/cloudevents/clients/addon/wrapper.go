package addon

import (
	"context"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1alpha1"
	addonv1beta1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1beta1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
)

// AddonClientSetWrapper wraps AddonV1Alpha1ClientWrapper to an addon clientset interface
type AddonClientSetWrapper struct {
	*AddonV1Alpha1ClientWrapper
}

var _ addonclientset.Interface = &AddonClientSetWrapper{}

func (a AddonClientSetWrapper) Discovery() discovery.DiscoveryInterface {
	panic("Discovery is unsupported")
}

func (a AddonClientSetWrapper) AddonV1alpha1() addonv1alpha1client.AddonV1alpha1Interface {
	return a.AddonV1Alpha1ClientWrapper
}

func (a AddonClientSetWrapper) AddonV1beta1() addonv1beta1client.AddonV1beta1Interface {
	panic("AddonV1beta1 is unsupported")
}

// AddonV1Alpha1ClientWrapper wraps ManagedClusterAddOnClient to AddonV1alpha1Interface
type AddonV1Alpha1ClientWrapper struct {
	*ManagedClusterAddOnClient
}

var _ addonv1alpha1client.AddonV1alpha1Interface = &AddonV1Alpha1ClientWrapper{}

func (c *AddonV1Alpha1ClientWrapper) AddOnDeploymentConfigs(namespace string) addonv1alpha1client.AddOnDeploymentConfigInterface {
	panic("AddOnDeploymentConfigs is unsupported")
}

func (c *AddonV1Alpha1ClientWrapper) AddOnTemplates() addonv1alpha1client.AddOnTemplateInterface {
	panic("AddOnTemplates is unsupported")
}

func (c *AddonV1Alpha1ClientWrapper) ClusterManagementAddOns() addonv1alpha1client.ClusterManagementAddOnInterface {
	panic("ClusterManagementAddOns is unsupported")
}

func (c *AddonV1Alpha1ClientWrapper) RESTClient() rest.Interface {
	panic("RESTClient is unsupported")
}

func (c *AddonV1Alpha1ClientWrapper) ManagedClusterAddOns(namespace string) addonv1alpha1client.ManagedClusterAddOnInterface {
	return c.ManagedClusterAddOnClient.Namespace(namespace)
}

// ManagedClusterAddOnInterface returns a client for ManagedClusterAddOn
func ManagedClusterAddOnInterface(ctx context.Context, opt *options.GenericClientOptions[*addonapiv1alpha1.ManagedClusterAddOn]) (addonclientset.Interface, error) {
	cloudEventsClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	addonClient := NewManagedClusterAddOnClient(cloudEventsClient, opt.WatcherStore())

	return &AddonClientSetWrapper{&AddonV1Alpha1ClientWrapper{addonClient}}, nil
}
