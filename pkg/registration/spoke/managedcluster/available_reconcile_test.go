package managedcluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func newHealthCheckServer(t *testing.T, livezStatus *atomic.Int32, healthzStatus *atomic.Int32, responseMsg string) (*httptest.Server, *discovery.DiscoveryClient) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/livez":
			status := int(livezStatus.Load())
			w.WriteHeader(status)
			if status != http.StatusOK {
				if _, err := w.Write([]byte(responseMsg)); err != nil {
					t.Fatal(err)
				}
			}
		case "/healthz":
			status := int(healthzStatus.Load())
			w.WriteHeader(status)
			if status != http.StatusOK {
				if _, err := w.Write([]byte(responseMsg)); err != nil {
					t.Fatal(err)
				}
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(&rest.Config{Host: apiServer.URL})
	return apiServer, discoveryClient
}

func newAtomicStatus(status int) *atomic.Int32 {
	v := &atomic.Int32{}
	v.Store(int32(status))
	return v
}

func TestAvailableReconcile(t *testing.T) {
	cases := []struct {
		name              string
		livezStatus       int
		healthzStatus     int
		responseMsg       string
		expectedStatus    metav1.ConditionStatus
		expectedReason    string
		expectedState     reconcileState
		expectedMsgPrefix string
	}{
		{
			name:           "kube-apiserver is healthy via livez",
			livezStatus:    http.StatusOK,
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "ManagedClusterAvailable",
			expectedState:  reconcileContinue,
		},
		{
			name:           "livez not found, fallback to healthz which is ok",
			livezStatus:    http.StatusNotFound,
			healthzStatus:  http.StatusOK,
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "ManagedClusterAvailable",
			expectedState:  reconcileContinue,
		},
		{
			name:           "livez forbidden, fallback to healthz which is ok",
			livezStatus:    http.StatusForbidden,
			healthzStatus:  http.StatusOK,
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "ManagedClusterAvailable",
			expectedState:  reconcileContinue,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			livez := newAtomicStatus(c.livezStatus)
			healthz := newAtomicStatus(c.healthzStatus)
			apiServer, discoveryClient := newHealthCheckServer(t, livez, healthz, c.responseMsg)
			defer apiServer.Close()

			reconciler := &availableReconcile{
				managedClusterDiscoveryClient: discoveryClient,
			}

			cluster := testinghelpers.NewAcceptedManagedCluster()
			updatedCluster, state, err := reconciler.reconcile(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), cluster)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if state != c.expectedState {
				t.Errorf("expected state %v, got %v", c.expectedState, state)
			}

			condition := meta.FindStatusCondition(updatedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			if condition == nil {
				t.Fatalf("expected Available condition to be set")
			}
			if condition.Status != c.expectedStatus {
				t.Errorf("expected condition status %v, got %v", c.expectedStatus, condition.Status)
			}
			if condition.Reason != c.expectedReason {
				t.Errorf("expected condition reason %q, got %q", c.expectedReason, condition.Reason)
			}
		})
	}
}

func TestAvailableReconcileFailureThreshold(t *testing.T) {
	livez := newAtomicStatus(http.StatusInternalServerError)
	healthz := newAtomicStatus(0)
	apiServer, discoveryClient := newHealthCheckServer(t, livez, healthz, "internal server error")
	defer apiServer.Close()

	// Use short durations for testing
	origThreshold := failureThreshold
	origInterval := healthCheckRetryInterval
	failureThreshold = 100 * time.Millisecond
	healthCheckRetryInterval = 50 * time.Millisecond
	defer func() {
		failureThreshold = origThreshold
		healthCheckRetryInterval = origInterval
	}()

	reconciler := &availableReconcile{
		managedClusterDiscoveryClient: discoveryClient,
	}

	// Set an initial Available=True condition to verify it's preserved during transient failures.
	cluster := testinghelpers.NewAcceptedManagedCluster()
	cluster.Status.Conditions = append(cluster.Status.Conditions, metav1.Condition{
		Type:    clusterv1.ManagedClusterConditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedClusterAvailable",
		Message: "Managed cluster is available",
	})

	// First failure: condition should remain True (within threshold), and a fast requeue should be triggered
	syncCtx := testingcommon.NewFakeSyncContext(t, "")
	updatedCluster, state, err := reconciler.reconcile(context.TODO(), syncCtx, cluster.DeepCopy())
	if err != nil {
		t.Fatalf("first failure: unexpected error: %v", err)
	}
	if state != reconcileStop {
		t.Errorf("first failure: expected reconcileStop, got %v", state)
	}
	condition := meta.FindStatusCondition(updatedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if condition == nil {
		t.Fatalf("first failure: expected Available condition")
	}
	if condition.Status != metav1.ConditionTrue {
		t.Errorf("first failure: expected condition to remain True, got %v", condition.Status)
	}

	// Simulate failure persisting beyond threshold
	reconciler.firstFailureTime = time.Now().Add(-failureThreshold)
	syncCtx = testingcommon.NewFakeSyncContext(t, "")
	updatedCluster, state, err = reconciler.reconcile(context.TODO(), syncCtx, cluster.DeepCopy())
	if err != nil {
		t.Fatalf("threshold failure: unexpected error: %v", err)
	}
	if state != reconcileStop {
		t.Errorf("threshold failure: expected reconcileStop, got %v", state)
	}
	condition = meta.FindStatusCondition(updatedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if condition == nil {
		t.Fatalf("threshold failure: expected Available condition")
	}
	if condition.Status != metav1.ConditionFalse {
		t.Errorf("threshold failure: expected condition False after threshold exceeded, got %v", condition.Status)
	}
	if condition.Reason != "ManagedClusterKubeAPIServerUnavailable" {
		t.Errorf("threshold failure: expected reason ManagedClusterKubeAPIServerUnavailable, got %q", condition.Reason)
	}
}

func TestAvailableReconcileFailureReset(t *testing.T) {
	livez := newAtomicStatus(http.StatusInternalServerError)
	healthz := newAtomicStatus(0)
	apiServer, discoveryClient := newHealthCheckServer(t, livez, healthz, "internal server error")
	defer apiServer.Close()

	reconciler := &availableReconcile{
		managedClusterDiscoveryClient: discoveryClient,
	}

	cluster := testinghelpers.NewAcceptedManagedCluster()
	cluster.Status.Conditions = append(cluster.Status.Conditions, metav1.Condition{
		Type:    clusterv1.ManagedClusterConditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedClusterAvailable",
		Message: "Managed cluster is available",
	})

	// First failure: sets firstFailureTime
	reconciler.reconcile(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), cluster.DeepCopy())
	if reconciler.firstFailureTime.IsZero() {
		t.Fatal("expected firstFailureTime to be set after failure")
	}

	// Recovery: API server becomes healthy, firstFailureTime should reset
	livez.Store(int32(http.StatusOK))
	updatedCluster, state, err := reconciler.reconcile(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), cluster.DeepCopy())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != reconcileContinue {
		t.Errorf("expected reconcileContinue after recovery, got %v", state)
	}
	if !reconciler.firstFailureTime.IsZero() {
		t.Error("expected firstFailureTime to be reset after recovery")
	}
	condition := meta.FindStatusCondition(updatedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		t.Errorf("expected condition True after recovery")
	}

	// Fail again: should start fresh, not carry over previous failure time
	livez.Store(int32(http.StatusInternalServerError))
	reconciler.reconcile(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), cluster.DeepCopy())

	// The failure just started, so even with many rapid calls it should not flip to False
	for i := 0; i < 10; i++ {
		updatedCluster, _, _ = reconciler.reconcile(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), cluster.DeepCopy())
	}
	condition = meta.FindStatusCondition(updatedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if condition.Status != metav1.ConditionTrue {
		t.Errorf("expected condition to remain True during fresh failure window, got %v", condition.Status)
	}
}

func TestAvailableReconcileBurstEvents(t *testing.T) {
	livez := newAtomicStatus(http.StatusInternalServerError)
	healthz := newAtomicStatus(0)
	apiServer, discoveryClient := newHealthCheckServer(t, livez, healthz, "internal server error")
	defer apiServer.Close()

	reconciler := &availableReconcile{
		managedClusterDiscoveryClient: discoveryClient,
	}

	cluster := testinghelpers.NewAcceptedManagedCluster()
	cluster.Status.Conditions = append(cluster.Status.Conditions, metav1.Condition{
		Type:    clusterv1.ManagedClusterConditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedClusterAvailable",
		Message: "Managed cluster is available",
	})

	// Simulate a burst of watch events (node + cluster + namespace) firing together.
	// All calls happen instantly, well within failureThreshold, so condition must stay True.
	for i := 0; i < 20; i++ {
		reconciler.reconcile(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), cluster.DeepCopy())
	}

	updatedCluster, _, _ := reconciler.reconcile(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), cluster.DeepCopy())
	condition := meta.FindStatusCondition(updatedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if condition.Status != metav1.ConditionTrue {
		t.Errorf("expected condition to remain True during burst, got %v", condition.Status)
	}
}
