package internal

import (
	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1alpha1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1alpha1"
	sourceclient "open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/client"
)

// WorkClientSetWrapper wraps a work client that has a manifestwork client to a work clientset interface, this wrapper
// will helps us to build manifestwork informer factory easily.
type WorkClientSetWrapper struct {
	WorkV1ClientWrapper *WorkV1ClientWrapper
}

var _ workclientset.Interface = &WorkClientSetWrapper{}

func (c *WorkClientSetWrapper) WorkV1() workv1client.WorkV1Interface {
	return c.WorkV1ClientWrapper
}

func (c *WorkClientSetWrapper) WorkV1alpha1() workv1alpha1client.WorkV1alpha1Interface {
	return nil
}

func (c *WorkClientSetWrapper) Discovery() discovery.DiscoveryInterface {
	return nil
}

// WorkV1ClientWrapper wraps a manifestwork client to a WorkV1Interface
type WorkV1ClientWrapper struct {
	ManifestWorkClient workv1client.ManifestWorkInterface
}

var _ workv1client.WorkV1Interface = &WorkV1ClientWrapper{}

func (c *WorkV1ClientWrapper) ManifestWorks(namespace string) workv1client.ManifestWorkInterface {
	if sourceManifestWorkClient, ok := c.ManifestWorkClient.(*sourceclient.ManifestWorkSourceClient); ok {
		sourceManifestWorkClient.SetNamespace(namespace)
		return sourceManifestWorkClient
	}
	return c.ManifestWorkClient
}

func (c *WorkV1ClientWrapper) AppliedManifestWorks() workv1client.AppliedManifestWorkInterface {
	return nil
}

func (c *WorkV1ClientWrapper) RESTClient() rest.Interface {
	return nil
}
