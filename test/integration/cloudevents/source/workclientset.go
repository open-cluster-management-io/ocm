package source

import (
	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1alpha1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1alpha1"
)

type workClientSetWrapper struct {
	WorkV1ClientWrapper *workV1ClientWrapper
}

var _ workclientset.Interface = &workClientSetWrapper{}

func (c *workClientSetWrapper) WorkV1() workv1client.WorkV1Interface {
	return c.WorkV1ClientWrapper
}

func (c *workClientSetWrapper) WorkV1alpha1() workv1alpha1client.WorkV1alpha1Interface {
	return nil
}

func (c *workClientSetWrapper) Discovery() discovery.DiscoveryInterface {
	return nil
}

type workV1ClientWrapper struct {
	ManifestWorkClient *manifestWorkSourceClient
}

var _ workv1client.WorkV1Interface = &workV1ClientWrapper{}

func (c *workV1ClientWrapper) ManifestWorks(namespace string) workv1client.ManifestWorkInterface {
	return c.ManifestWorkClient.SetNamespace(namespace)
}

func (c *workV1ClientWrapper) AppliedManifestWorks() workv1client.AppliedManifestWorkInterface {
	return nil
}

func (c *workV1ClientWrapper) RESTClient() rest.Interface {
	return nil
}
