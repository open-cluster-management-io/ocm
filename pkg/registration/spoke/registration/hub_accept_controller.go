package registration

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
)

// hubAcceptController watch ManagedCluster CR on hub after spoke bootstrap done
// If the managedCluster.Spec.HubAccetpsClient is false, then mark the connection as failed.
//
// Note that: when the HubAcceptsClient is false, the clusterrole and clusterrolebinding will be removed.
// Then the agent will not be able to get or watch the managedCluster CR. This is why we need to resync every minute and check if err is forbidden.
// That means the controller can only handle the case when the HubAcceptsClient change from true to false.
type hubAcceptController struct {
	clusterName       string
	hubClusterClient  clientset.Interface
	handleAcceptFalse func(ctx context.Context) error
	recorder          events.Recorder
}

func NewHubAcceptController(clusterName string, hubClusterInformer clusterv1informer.ManagedClusterInformer,
	hubClusterClient clientset.Interface,
	handleAcceptFalse func(ctx context.Context) error, recorder events.Recorder) factory.Controller {
	c := &hubAcceptController{
		clusterName:       clusterName,
		hubClusterClient:  hubClusterClient,
		handleAcceptFalse: handleAcceptFalse,
		recorder:          recorder,
	}
	return factory.New().
		WithInformers(hubClusterInformer.Informer()).
		ResyncEvery(3*time.Minute).
		WithSync(c.sync).
		ToController("HubAcceptController", recorder)
}

func (c *hubAcceptController) sync(ctx context.Context, _ factory.SyncContext) error {
	cluster, err := c.hubClusterClient.ClusterV1().ManagedClusters().Get(ctx, c.clusterName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			// In case the role is removed before the event is propagated, we also need to handle the forbidden case.
			return c.handleAcceptFalse(ctx)
		}
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
