package managedclusterset

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	defaultManagedClusterSetValue = "default"
)

// defaultManagedClusterSetLabelController is used to add a "default" clusterset label to managedcluster
// to add it to default managed cluster set if it it not belongs to any cluster set and have no cluster
// set label.
// This controller would be removed in next release.
//
// defaultManagedClusterSetLabelController reconciles ManagedClusterSet label.
type defaultManagedClusterSetLabelController struct {
	clusterClient clientset.Interface
	clusterLister clusterlisterv1.ManagedClusterLister
	eventRecorder events.Recorder
}

// NewDefaultManagedClusterSetLabelController creates a new set ManagedClusterSet label controller
func NewDefaultManagedClusterSetLabelController(
	clusterClient clientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	recorder events.Recorder) factory.Controller {

	c := &defaultManagedClusterSetLabelController{
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		eventRecorder: recorder.WithComponentSuffix("set-default-managed-cluster-set-label-controller"),
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("DefaultManagedClusterSetLabelController", recorder)
}

func (c *defaultManagedClusterSetLabelController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedClusterName := syncCtx.QueueKey()

	klog.V(4).Infof("Reconciling ManagedClusterSetLabel of ManagedCluster %s", managedClusterName)

	managedCluster, err := c.clusterLister.Get(managedClusterName)
	if errors.IsNotFound(err) {
		// Spoke cluster not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	if !managedCluster.DeletionTimestamp.IsZero() {
		// ManagedCluster is deleting, do nothing
		return nil
	}

	if err := c.syncClusterSetLabel(ctx, managedCluster); err != nil {
		return fmt.Errorf("failed to set default ManagedClusterSet label to ManagedCluster %s: %w", managedCluster.Name, err)
	}
	return nil
}

func (c *defaultManagedClusterSetLabelController) syncClusterSetLabel(ctx context.Context, managedCluster *clusterv1.ManagedCluster) error {
	cluster := managedCluster.DeepCopy()

	if v, ok := cluster.Labels[clusterSetLabel]; !ok || v == "" {
		modified := false

		clusterSetLabels := map[string]string{}
		clusterSetLabels[clusterSetLabel] = defaultManagedClusterSetValue
		// merge clusterSetLabel into ManagedCluster.Labels
		resourcemerge.MergeMap(&modified, &cluster.Labels, clusterSetLabels)

		// no work if the cluster labels have no change
		if !modified {
			return nil
		}

		// update ManagedCluster Labels
		_, err := c.clusterClient.ClusterV1().ManagedClusters().Update(ctx, cluster, metav1.UpdateOptions{})
		return err
	}

	// if clusterSetLabel already set, do nothing
	return nil
}
