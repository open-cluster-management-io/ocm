package addon

import (
	"context"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1beta1"

	"k8s.io/client-go/discovery"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1alpha1"
	addonv1v1beta1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1beta1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
)

// AddonClientSetWrapper wraps addon v1alpha1/v1beta1 client to an addon clientset interface
type AddonClientSetWrapper struct {
	alphaClient *v1alpha1.AddonClientWrapper
	betaClient  *v1beta1.AddonClientWrapper
}

var _ addonclientset.Interface = &AddonClientSetWrapper{}

func (a AddonClientSetWrapper) Discovery() discovery.DiscoveryInterface {
	panic("Discovery is unsupported")
}

func (a AddonClientSetWrapper) AddonV1alpha1() addonv1alpha1client.AddonV1alpha1Interface {
	return a.alphaClient
}

func (a AddonClientSetWrapper) AddonV1beta1() addonv1v1beta1client.AddonV1beta1Interface {
	if a.betaClient == nil {
		panic("AddonV1beta1 is not initialized")
	}
	return a.betaClient
}

// ManagedClusterAddOnInterface returns a client for ManagedClusterAddOn
func ManagedClusterAddOnInterface(ctx context.Context, opt *options.GenericClientOptions[*addonapiv1alpha1.ManagedClusterAddOn]) (addonclientset.Interface, error) {
	cloudEventsClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	addonClient := v1alpha1.NewManagedClusterAddOnClient(cloudEventsClient, opt.WatcherStore())

	// TODO switch to v1beta1
	return &AddonClientSetWrapper{alphaClient: v1alpha1.NewAddonClientWrapper(addonClient)}, nil
}
