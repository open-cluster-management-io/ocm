package managedcluster

import (
	"context"
	"fmt"
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
	existingCluster, err := c.hubClusterClient.ClusterV1().ManagedClusters().Get(ctx, c.clusterName, metav1.GetOptions{})
	// ManagedCluster is only allowed created during bootstrap. After bootstrap secret expired, an unauthorized error will be got, output log at the debug level
	if err != nil && skipUnauthorizedError(err) == nil {
		klog.V(4).Infof("unable to get the managed cluster %q from hub: %v", c.clusterName, err)
		return nil
	}

	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// create ManagedCluster if not found
	if errors.IsNotFound(err) {
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: c.clusterName,
			},
		}

		if len(c.spokeExternalServerURLs) != 0 {
			var managedClusterClientConfigs []clusterv1.ClientConfig
			for _, serverURL := range c.spokeExternalServerURLs {
				managedClusterClientConfigs = append(managedClusterClientConfigs, clusterv1.ClientConfig{
					URL:      serverURL,
					CABundle: c.spokeCABundle,
				})
			}
			managedCluster.Spec.ManagedClusterClientConfigs = managedClusterClientConfigs
		}

		_, err = c.hubClusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		// ManagedCluster is only allowed created during bootstrap. After bootstrap secret expired, an unauthorized error will be got, skip it
		if skipUnauthorizedError(err) != nil {
			return fmt.Errorf("unable to create managed cluster with name %q on hub: %w", c.clusterName, err)
		}
		syncCtx.Recorder().Eventf("ManagedClusterCreated", "Managed cluster %q created on hub", c.clusterName)
		return nil
	}

	// do not update ManagedClusterClientConfigs in ManagedCluster if spokeExternalServerURLs is empty
	if len(c.spokeExternalServerURLs) == 0 {
		return nil
	}

	// merge ClientConfig
	managedClusterClientConfigs := existingCluster.Spec.ManagedClusterClientConfigs
	for _, serverURL := range c.spokeExternalServerURLs {
		isIncludeByExisting := false
		for _, existingClientConfig := range existingCluster.Spec.ManagedClusterClientConfigs {
			if serverURL == existingClientConfig.URL {
				isIncludeByExisting = true
				break
			}
		}

		if !isIncludeByExisting {
			managedClusterClientConfigs = append(managedClusterClientConfigs, clusterv1.ClientConfig{
				URL:      serverURL,
				CABundle: c.spokeCABundle,
			})
		}
	}
	if len(existingCluster.Spec.ManagedClusterClientConfigs) == len(managedClusterClientConfigs) {
		return nil
	}

	// update ManagedClusterClientConfigs in ManagedCluster
	clusterCopy := existingCluster.DeepCopy()
	clusterCopy.Spec.ManagedClusterClientConfigs = managedClusterClientConfigs
	_, err = c.hubClusterClient.ClusterV1().ManagedClusters().Update(ctx, clusterCopy, metav1.UpdateOptions{})
	// ManagedClusterClientConfigs in ManagedCluster is only allowed updated during bootstrap. After bootstrap secret expired, an unauthorized error will be got, skip it
	if skipUnauthorizedError(err) != nil {
		return fmt.Errorf("unable to update ManagedClusterClientConfigs of managed cluster %q in hub: %w", c.clusterName, err)
	}

	return nil
}

func skipUnauthorizedError(err error) error {
	if errors.IsUnauthorized(err) || errors.IsForbidden(err) {
		return nil
	}

	return err
}
