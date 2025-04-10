package addon

import (
	"context"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned/typed/addon/v1alpha1"

	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
)

// ClientHolder holds a ManagedCluster client that implements the ManagedClusterInterface based on different configuration
//
// ClientHolder also implements the ManagedClustersGetter interface.
type ClientHolder struct {
	addonClientSet addonclientset.Interface
}

var _ addonv1alpha1client.ManagedClusterAddOnsGetter = &ClientHolder{}

// ClusterInterface returns a clusterclientset Interface
func (h *ClientHolder) ClusterInterface() addonclientset.Interface {
	return h.addonClientSet
}

// ManagedClusters returns a ManagedClusterInterface
func (h *ClientHolder) ManagedClusterAddOns(namespace string) addonv1alpha1client.ManagedClusterAddOnInterface {
	return h.addonClientSet.AddonV1alpha1().ManagedClusterAddOns(namespace)
}

// NewClientHolder returns a ClientHolder for ManagedCluster
func NewClientHolder(ctx context.Context, opt *options.GenericClientOptions[*addonapiv1alpha1.ManagedClusterAddOn]) (*ClientHolder, error) {

	// start to subscribe
	cloudEventsClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	managedClusterClientAddon := NewManagedClusterAddOnClient(cloudEventsClient, opt.WatcherStore())
	addonClient := &AddonV1Alpha1ClientWrapper{ManagedClusterAddonClient: managedClusterClientAddon}
	addonClientSet := &AddonClientSetWrapper{AddonV1Alpha1ClientWrapper: addonClient}

	return &ClientHolder{addonClientSet: addonClientSet}, nil
}
