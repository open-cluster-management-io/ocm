package managedcluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// failureThreshold is the duration that health check failures must persist
// before marking the cluster as unavailable. This prevents transient API server blips
// from flipping the Available condition.
var failureThreshold = 20 * time.Second

// healthCheckRetryInterval is the interval for fast requeue when a transient failure is
// detected, allowing sustained failures to be confirmed quickly.
var healthCheckRetryInterval = 5 * time.Second

type availableReconcile struct {
	managedClusterDiscoveryClient discovery.DiscoveryInterface
	firstFailureTime              time.Time
}

func (r *availableReconcile) reconcile(ctx context.Context, syncCtx factory.SyncContext, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	condition := r.checkKubeAPIServerStatus(ctx)

	if condition.Status == metav1.ConditionTrue {
		r.firstFailureTime = time.Time{}
		meta.SetStatusCondition(&cluster.Status.Conditions, condition)
		return cluster, reconcileContinue, nil
	}

	now := time.Now()
	if r.firstFailureTime.IsZero() {
		r.firstFailureTime = now
	}

	if now.Sub(r.firstFailureTime) >= failureThreshold {
		meta.SetStatusCondition(&cluster.Status.Conditions, condition)
		return cluster, reconcileStop, nil
	}

	// Transient failure: keep the existing condition unchanged, but requeue quickly
	// to confirm whether the failure is sustained.
	syncCtx.Queue().AddAfter("", healthCheckRetryInterval)
	return cluster, reconcileStop, nil
}

// checkKubeAPIServerStatus uses the livez api to check the status of kube apiserver with healthz as fallback
func (r *availableReconcile) checkKubeAPIServerStatus(ctx context.Context) metav1.Condition {
	statusCode := 0
	condition := metav1.Condition{Type: clusterv1.ManagedClusterConditionAvailable}
	result := r.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/livez").Do(ctx).StatusCode(&statusCode)
	if statusCode == http.StatusOK {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ManagedClusterAvailable"
		condition.Message = "Managed cluster is available"
		return condition
	}

	// for backward compatible, the livez endpoint is supported from Kubernetes 1.16, so if the livez is not found or
	// forbidden, the healthz endpoint will be used.
	if statusCode == http.StatusNotFound || statusCode == http.StatusForbidden {
		result = r.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/healthz").Do(ctx).StatusCode(&statusCode)
		if statusCode == http.StatusOK {
			condition.Status = metav1.ConditionTrue
			condition.Reason = "ManagedClusterAvailable"
			condition.Message = "Managed cluster is available"
			return condition
		}
	}

	condition.Status = metav1.ConditionFalse
	condition.Reason = "ManagedClusterKubeAPIServerUnavailable"
	body, err := result.Raw()
	if err == nil {
		condition.Message = fmt.Sprintf("The kube-apiserver is not ok, status code: %d, %v", statusCode, string(body))
		return condition
	}

	condition.Message = fmt.Sprintf("The kube-apiserver is not ok, status code: %d, %v", statusCode, err)
	return condition
}
