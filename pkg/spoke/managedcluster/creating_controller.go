package managedcluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// well-known anonymous user
const anonymous = "system:anonymous"

var (
	// CreatingControllerSyncInterval is exposed so that integration tests can crank up the controller sync speed.
	CreatingControllerSyncInterval = 60 * time.Minute
)

// managedClusterCreatingController creates a ManagedCluster on hub cluster during the spoke agent bootstrap phase
type managedClusterCreatingController struct {
	clusterName             string
	spokeExternalServerURLs []string
	spokeCABundle           []byte
	hubClusterClient        clientset.Interface
}

// NewManagedClusterCreatingController creates a new managedClusterCreatingController on the managed cluster.
func NewManagedClusterCreatingController(
	clusterName string, spokeExternalServerURLs []string,
	spokeCABundle []byte,
	hubClusterClient clientset.Interface,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterCreatingController{
		clusterName:             clusterName,
		spokeExternalServerURLs: spokeExternalServerURLs,
		spokeCABundle:           spokeCABundle,
		hubClusterClient:        hubClusterClient,
	}

	return factory.New().
		WithSync(c.sync).
		ResyncEvery(wait.Jitter(CreatingControllerSyncInterval, 1.0)).
		ToController("ManagedClusterCreatingController", recorder)
}

func (c *managedClusterCreatingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	_, err := c.hubClusterClient.ClusterV1().ManagedClusters().Get(ctx, c.clusterName, metav1.GetOptions{})
	switch {
	case errors.IsUnauthorized(err),
		errors.IsForbidden(err) && strings.Contains(err.Error(), anonymous):
		klog.V(4).Infof("unable to get the managed cluster %q from hub: %v", c.clusterName, err)
		return nil
	case errors.IsNotFound(err):
	case err == nil:
		return nil
	case err != nil:
		return err
	}

	managedCluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.clusterName,
		},
	}

	if len(c.spokeExternalServerURLs) != 0 {
		managedClusterClientConfigs := []clusterv1.ClientConfig{}
		for _, serverURL := range c.spokeExternalServerURLs {
			managedClusterClientConfigs = append(managedClusterClientConfigs, clusterv1.ClientConfig{
				URL:      serverURL,
				CABundle: c.spokeCABundle,
			})
		}
		managedCluster.Spec.ManagedClusterClientConfigs = managedClusterClientConfigs
	}

	_, err = c.hubClusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("unable to create managed cluster with name %q on hub: %w", c.clusterName, err)
	}
	syncCtx.Recorder().Eventf("ManagedClusterCreated", "Managed cluster %q created on hub", c.clusterName)
	return nil
}
