package registration

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
)

var (
	// CreatingControllerSyncInterval is exposed so that integration tests can crank up the controller sync speed.
	CreatingControllerSyncInterval = 60 * time.Minute
)

type ManagedClusterDecorator func(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster

// managedClusterCreatingController creates a ManagedCluster on hub cluster during the spoke agent bootstrap phase
type managedClusterCreatingController struct {
	clusterName       string
	clusterDecorators []ManagedClusterDecorator
	hubClusterClient  clientset.Interface
}

// NewManagedClusterCreatingController creates a new managedClusterCreatingController on the managed cluster.
func NewManagedClusterCreatingController(
	clusterName string,
	decorators []ManagedClusterDecorator,
	hubClusterClient clientset.Interface) factory.Controller {

	c := &managedClusterCreatingController{
		clusterName:       clusterName,
		hubClusterClient:  hubClusterClient,
		clusterDecorators: decorators,
	}

	return factory.New().
		WithSync(c.sync).
		ResyncEvery(wait.Jitter(CreatingControllerSyncInterval, 1.0)).
		ToController("ManagedClusterCreatingController")
}

func (c *managedClusterCreatingController) sync(ctx context.Context, syncCtx factory.SyncContext, _ string) error {
	logger := klog.FromContext(ctx)
	existingCluster, err := c.hubClusterClient.ClusterV1().ManagedClusters().Get(ctx, c.clusterName, metav1.GetOptions{})
	// ManagedCluster is only allowed created during bootstrap. After bootstrap secret expired, an unauthorized error will be got, output log at the debug level
	if err != nil && skipUnauthorizedError(err) == nil {
		logger.V(4).Info("unable to get the managed cluster from hub", "clusterName", c.clusterName, "err", err)
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

		for _, decorator := range c.clusterDecorators {
			managedCluster = decorator(managedCluster)
		}

		_, err = c.hubClusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			logger.V(4).Info("managed cluster already exists", "clusterName", c.clusterName)
			return nil
		}

		if err != nil {
			// Unauthorized/Forbidden after bootstrap: skip without emitting a create event.
			if skipUnauthorizedError(err) == nil {
				return nil
			}
			return fmt.Errorf("unable to create managed cluster with name %q on hub: %w", c.clusterName, err)
		}
		syncCtx.Recorder().Eventf(ctx, "ManagedClusterCreated", "Managed cluster %q created on hub", c.clusterName)
		return nil
	}

	managedCluster := existingCluster.DeepCopy()
	for _, decorator := range c.clusterDecorators {
		managedCluster = decorator(managedCluster)
	}

	if len(existingCluster.Spec.ManagedClusterClientConfigs) == len(managedCluster.Spec.ManagedClusterClientConfigs) {
		return nil
	}

	// update ManagedClusterClientConfigs in ManagedCluster
	_, err = c.hubClusterClient.ClusterV1().ManagedClusters().Update(ctx, managedCluster, metav1.UpdateOptions{})
	// ManagedClusterClientConfigs in ManagedCluster is only allowed updated during bootstrap.
	// After bootstrap secret expired, an unauthorized error will be got, skip it
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

func AnnotationDecorator(annotations map[string]string) ManagedClusterDecorator {
	return func(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
		filteredAnnotations := commonhelpers.FilterClusterAnnotations(annotations)
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		for key, value := range filteredAnnotations {
			cluster.Annotations[key] = value
		}
		return cluster
	}
}

// ClientConfigDecorator merge ClientConfig
func ClientConfigDecorator(externalServerURLs []string, caBundle []byte) ManagedClusterDecorator {
	return func(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
		for _, serverURL := range externalServerURLs {
			isIncludeByExisting := false
			for _, existingClientConfig := range cluster.Spec.ManagedClusterClientConfigs {
				if serverURL == existingClientConfig.URL {
					isIncludeByExisting = true
					break
				}
			}

			if !isIncludeByExisting {
				cluster.Spec.ManagedClusterClientConfigs = append(
					cluster.Spec.ManagedClusterClientConfigs, clusterv1.ClientConfig{
						URL:      serverURL,
						CABundle: caBundle,
					})
			}
		}
		return cluster
	}
}
