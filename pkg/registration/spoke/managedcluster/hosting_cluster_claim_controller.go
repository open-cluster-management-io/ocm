package managedcluster

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	clusterv1alpha1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1alpha1"
	clusterv1alpha1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// HostingClusterClaimName is the reserved self-report claim (KEP-188).
const HostingClusterClaimName = "hosting-cluster.open-cluster-management.io"

// hostingClusterClaimResync is a safety-net resync; the informer watch below reacts immediately.
const hostingClusterClaimResync = 5 * time.Minute

// hostingClusterClaimController is only started when reportHostingCluster is enabled.
type hostingClusterClaimController struct {
	claimClient        clusterv1alpha1client.ClusterClaimInterface
	hostingClusterName string
}

// NewHostingClusterClaimController keeps the reserved claim in sync with hostingClusterName.
func NewHostingClusterClaimController(
	claimClient clusterv1alpha1client.ClusterClaimInterface,
	claimInformer clusterv1alpha1informer.ClusterClaimInformer,
	hostingClusterName string,
) factory.Controller {
	c := &hostingClusterClaimController{
		claimClient:        claimClient,
		hostingClusterName: hostingClusterName,
	}
	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			factory.DefaultQueueKeysFunc,
			isHostingClusterClaim,
			claimInformer.Informer(),
		).
		WithSync(c.sync).
		ResyncEvery(hostingClusterClaimResync).
		ToController("HostingClusterClaimController")
}

func isHostingClusterClaim(obj interface{}) bool {
	accessor, err := meta.Accessor(obj)
	return err == nil && accessor.GetName() == HostingClusterClaimName
}

func (c *hostingClusterClaimController) sync(ctx context.Context, _ factory.SyncContext, _ string) error {
	logger := klog.FromContext(ctx)

	if c.hostingClusterName == "" {
		// Cleared value: delete rather than publish an empty (schema-invalid) claim.
		err := c.claimClient.Delete(ctx, HostingClusterClaimName, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	existing, err := c.claimClient.Get(ctx, HostingClusterClaimName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		_, err = c.claimClient.Create(ctx, &clusterv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{Name: HostingClusterClaimName},
			Spec:       clusterv1alpha1.ClusterClaimSpec{Value: c.hostingClusterName},
		}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		logger.V(2).Info("Created hosting-cluster self-report claim", "value", c.hostingClusterName)
		return nil
	case err != nil:
		return err
	case existing.Spec.Value == c.hostingClusterName:
		return nil
	}

	updated := existing.DeepCopy()
	updated.Spec.Value = c.hostingClusterName
	if _, err := c.claimClient.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return err
	}
	logger.V(2).Info("Updated hosting-cluster self-report claim", "value", c.hostingClusterName)
	return nil
}
