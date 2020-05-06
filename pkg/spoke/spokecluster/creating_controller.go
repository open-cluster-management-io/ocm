package spokecluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// well-known anonymous user
const anonymous = "system:anonymous"

// CreatingControllerSyncInterval is exposed so that integration tests can crank up the constroller sync speed.
var CreatingControllerSyncInterval = 60 * time.Minute

// spokeClusterCreatingController creates a spoke cluster on hub cluster during the spoke agent bootstrap phase
type spokeClusterCreatingController struct {
	clusterName             string
	spokeExternalServerURLs []string
	spokeCABundle           []byte
	hubClusterClient        clientset.Interface
}

// NewSpokeClusterCreatingController creates a new spoke cluster creating controller on the spoke cluster.
func NewSpokeClusterCreatingController(
	clusterName string, spokeExternalServerURLs []string,
	spokeCABundle []byte,
	hubClusterClient clientset.Interface,
	recorder events.Recorder) factory.Controller {
	c := &spokeClusterCreatingController{
		clusterName:             clusterName,
		spokeExternalServerURLs: spokeExternalServerURLs,
		spokeCABundle:           spokeCABundle,
		hubClusterClient:        hubClusterClient,
	}
	return factory.New().
		WithSync(c.sync).
		ResyncEvery(CreatingControllerSyncInterval).
		ToController("SpokeClusterCreatingController", recorder)
}

func (c *spokeClusterCreatingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	_, err := c.hubClusterClient.ClusterV1().SpokeClusters().Get(ctx, c.clusterName, metav1.GetOptions{})
	switch {
	case errors.IsUnauthorized(err),
		errors.IsForbidden(err) && strings.Contains(err.Error(), anonymous):
		klog.V(4).Infof("unable to get the spoke cluster %q from hub: %v", c.clusterName, err)
		return nil
	case errors.IsNotFound(err):
	case err == nil:
		return nil
	case err != nil:
		return err
	}

	spokeCluster := &clusterv1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.clusterName,
		},
	}

	if len(c.spokeExternalServerURLs) != 0 {
		spokeClientConfigs := []clusterv1.ClientConfig{}
		for _, serverURL := range c.spokeExternalServerURLs {
			spokeClientConfigs = append(spokeClientConfigs, clusterv1.ClientConfig{
				URL:      serverURL,
				CABundle: c.spokeCABundle,
			})
		}
		spokeCluster.Spec.SpokeClientConfigs = spokeClientConfigs
	}

	_, err = c.hubClusterClient.ClusterV1().SpokeClusters().Create(ctx, spokeCluster, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("unable to create spoke cluster with name %q on hub: %w", c.clusterName, err)
	}
	syncCtx.Recorder().Eventf("SpokeClusterCreated", "Spoke cluster %q created on hub", c.clusterName)
	return nil
}
