package spokecluster

import (
	"context"
	"fmt"
	"time"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// spokeClusterCreatingController creates a spoke cluster on hub cluster during the spoke agent bootstrap phase
type spokeClusterCreatingController struct {
	isSpokeClusterExists   bool
	clusterName            string
	spokeExternalServerUrl string
	spokeCABundle          []byte
	hasSpokeClusterFunc    func(bool)
	hubClusterClient       clientset.Interface
}

// NewSpokeClusterCreatingController creates a new spoke cluster creating controller on the spoke cluster.
func NewSpokeClusterCreatingController(
	clusterName, spokeExternalServerUrl string,
	spokeCABundle []byte,
	hubClusterClient clientset.Interface,
	hasSpokeClusterFunc func(bool),
	recorder events.Recorder) factory.Controller {
	c := &spokeClusterCreatingController{
		clusterName:            clusterName,
		spokeExternalServerUrl: spokeExternalServerUrl,
		spokeCABundle:          spokeCABundle,
		hasSpokeClusterFunc:    hasSpokeClusterFunc,
		hubClusterClient:       hubClusterClient,
	}
	return factory.New().
		WithSync(c.sync).
		ResyncEvery(5*time.Minute).
		ToController("SpokeClusterCreatingController", recorder)
}

func (c *spokeClusterCreatingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	if c.isSpokeClusterExists {
		return nil
	}

	spokeCluster := &clusterv1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.clusterName,
		},
		Spec: clusterv1.SpokeClusterSpec{
			SpokeClientConfig: clusterv1.ClientConfig{
				URL:      c.spokeExternalServerUrl,
				CABundle: c.spokeCABundle,
			},
		},
	}

	_, err := c.hubClusterClient.ClusterV1().SpokeClusters().Create(ctx, spokeCluster, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		c.isSpokeClusterExists = true
		c.hasSpokeClusterFunc(c.isSpokeClusterExists)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to create spoke cluster with name %q on hub: %w", c.clusterName, err)
	}

	c.isSpokeClusterExists = true
	c.hasSpokeClusterFunc(c.isSpokeClusterExists)
	syncCtx.Recorder().Eventf("SpokeClusterCreated", "Spoke cluster %q created on hub", c.clusterName)
	return nil
}
