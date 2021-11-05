package managedcluster

import (
	"context"
	"fmt"
	"time"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// managedClusterJoiningController add the joined condition to a ManagedCluster on the managed cluster after it is accepted by hub cluster admin.
type managedClusterJoiningController struct {
	clusterName      string
	hubClusterClient clientset.Interface
	hubClusterLister clusterv1listers.ManagedClusterLister
}

// NewManagedClusterJoiningController creates a new managed cluster joining controller on the managed cluster.
func NewManagedClusterJoiningController(
	clusterName string,
	hubClusterClient clientset.Interface,
	hubManagedClusterInformer clusterv1informer.ManagedClusterInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterJoiningController{
		clusterName:      clusterName,
		hubClusterClient: hubClusterClient,
		hubClusterLister: hubManagedClusterInformer.Lister(),
	}

	return factory.New().
		WithInformers(hubManagedClusterInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(5*time.Minute).
		ToController("ManagedClusterJoiningController", recorder)
}

// sync maintains the managed cluster side status of a ManagedCluster, it maintains the ManagedClusterJoined condition according to
// the value of the ManagedClusterHubAccepted condition.
func (c managedClusterJoiningController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedCluster, err := c.hubClusterLister.Get(c.clusterName)
	if err != nil {
		return fmt.Errorf("unable to get managed cluster with name %q from hub: %w", c.clusterName, err)
	}

	// current managed cluster is not accepted, do nothing.
	if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
		syncCtx.Recorder().Eventf("ManagedClusterIsNotAccepted", "Managed cluster %q is not accepted by hub yet", c.clusterName)
		return nil
	}

	joined := meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
	if joined {
		// current managed cluster is joined, do nothing.
		return nil
	}

	// current managed cluster did not join the hub cluster, join it.
	_, updated, err := helpers.UpdateManagedClusterStatus(
		ctx,
		c.hubClusterClient,
		c.clusterName,
		helpers.UpdateManagedClusterConditionFn(metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionJoined,
			Status:  metav1.ConditionTrue,
			Reason:  "ManagedClusterJoined",
			Message: "Managed cluster joined",
		}),
	)
	if err != nil {
		return fmt.Errorf("unable to update status of managed cluster %q: %w", c.clusterName, err)
	}
	if updated {
		syncCtx.Recorder().Eventf("ManagedClusterJoined", "Managed cluster %q joined hub", c.clusterName)
	}
	return nil
}
