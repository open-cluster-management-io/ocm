package registration

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

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
	hubClusterClient clientset.Interface,
	recorder events.Recorder) factory.Controller {

	c := &managedClusterCreatingController{
		clusterName:       clusterName,
		hubClusterClient:  hubClusterClient,
		clusterDecorators: decorators,
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

		for _, decorator := range c.clusterDecorators {
			managedCluster = decorator(managedCluster)
		}

		_, err = c.hubClusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		// ManagedCluster is only allowed created during bootstrap. After bootstrap secret expired, an unauthorized error will be got, skip it
		if skipUnauthorizedError(err) != nil {
			return fmt.Errorf("unable to create managed cluster with name %q on hub: %w", c.clusterName, err)
		}
		syncCtx.Recorder().Eventf("ManagedClusterCreated", "Managed cluster %q created on hub", c.clusterName)
		return nil
	}

	managedCluster := existingCluster.DeepCopy()
	for _, decorator := range c.clusterDecorators {
		managedCluster = decorator(managedCluster)
	}

	// Check if any updates are needed (ClientConfigs, Labels, or Annotations)
	if equalClientConfigs(existingCluster.Spec.ManagedClusterClientConfigs, managedCluster.Spec.ManagedClusterClientConfigs) &&
		equalLabels(existingCluster.Labels, managedCluster.Labels) &&
		equalAnnotations(existingCluster.Annotations, managedCluster.Annotations) {
		return nil
	}

	// update ManagedCluster (ClientConfigs, Labels, and Annotations)
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

// AnnotationDecorator set annotations from annotation map to ManagedCluster
func AnnotationDecorator(annotations map[string]string) ManagedClusterDecorator {
	return func(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}

		filteredAnnotations := commonhelpers.FilterClusterAnnotations(annotations)
		for k, v := range filteredAnnotations {
			cluster.Annotations[k] = v
		}

		return cluster
	}
}

// LabelDecorator set labels from label map to ManagedCluster
func LabelDecorator(labels map[string]string) ManagedClusterDecorator {
	return func(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}

		for k, v := range labels {
			cluster.Labels[k] = v
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

// equalClientConfigs compares two ClientConfig slices for equality
func equalClientConfigs(configs1, configs2 []clusterv1.ClientConfig) bool {
	if len(configs1) != len(configs2) {
		return false
	}
	for i, config1 := range configs1 {
		config2 := configs2[i]
		if config1.URL != config2.URL {
			return false
		}
		if !bytes.Equal(config1.CABundle, config2.CABundle) {
			return false
		}
	}
	return true
}

// equalLabels compares two label maps for equality
func equalLabels(labels1, labels2 map[string]string) bool {
	if len(labels1) != len(labels2) {
		return false
	}
	for k, v := range labels1 {
		if labels2[k] != v {
			return false
		}
	}
	return true
}

// equalAnnotations compares two annotation maps for equality
func equalAnnotations(annotations1, annotations2 map[string]string) bool {
	if len(annotations1) != len(annotations2) {
		return false
	}
	for k, v := range annotations1 {
		if annotations2[k] != v {
			return false
		}
	}
	return true
}
