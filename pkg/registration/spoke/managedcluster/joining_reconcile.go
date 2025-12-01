package managedcluster

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

type joiningReconcile struct{}

func (r *joiningReconcile) reconcile(ctx context.Context, syncCtx factory.SyncContext, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	// current managed cluster is not accepted, do nothing.
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
		syncCtx.Recorder().Eventf(ctx, "ManagedClusterIsNotAccepted", "Managed cluster %q is not accepted by hub yet", cluster.Name)
		return cluster, reconcileStop, nil
	}

	joined := meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
	if joined {
		// current managed cluster is joined, do nothing.
		return cluster, reconcileContinue, nil
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    clusterv1.ManagedClusterConditionJoined,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedClusterJoined",
		Message: "Managed cluster joined",
	})

	return cluster, reconcileContinue, nil
}
