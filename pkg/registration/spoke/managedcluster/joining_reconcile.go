package managedcluster

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type joiningReconcile struct {
	recorder events.Recorder
}

func (r *joiningReconcile) reconcile(ctx context.Context, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	// current managed cluster is not accepted, do nothing.
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
		r.recorder.Eventf("ManagedClusterIsNotAccepted", "Managed cluster %q is not accepted by hub yet", cluster.Name)
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
