package registration

import (
	"context"

	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// hubAcceptController watch ManagedCluster CR on hub after spoke bootstrap done
// If the managedCluster.Spec.HubAccetpsClient is false, then mark the connection as failed.
//
// Note that: when the HubAcceptsClient is false, the clusterrole and clusterrolebinding will be removed.
// Then the agent will not be able to get or watch the managedCluster CR.
// That means the controller can only handle the case when the HubAcceptsClient change from true to false.
type hubAcceptController struct {
	clusterName       string
	hubClusterLister  clusterv1listers.ManagedClusterLister
	handleAcceptFalse func(ctx context.Context) error
}

func NewHubAcceptController(clusterName string, hubClusterInformer clusterv1informer.ManagedClusterInformer,
	handleAcceptFalse func(ctx context.Context) error) factory.Controller {
	c := &hubAcceptController{
		clusterName:       clusterName,
		hubClusterLister:  hubClusterInformer.Lister(),
		handleAcceptFalse: handleAcceptFalse,
	}
	return factory.New().
		WithInformers(hubClusterInformer.Informer()).
		WithSync(c.sync).
		ToController("HubAcceptController")
}

func (c *hubAcceptController) sync(ctx context.Context, _ factory.SyncContext, _ string) error {
	cluster, err := c.hubClusterLister.Get(c.clusterName)
	if err != nil {
		return err
	}
	if !cluster.Spec.HubAcceptsClient {
		if c.handleAcceptFalse == nil {
			return nil
		}
		return c.handleAcceptFalse(ctx)
	}
	return nil
}
