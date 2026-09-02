package managedcluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestHostingClusterClaimControllerCreate(t *testing.T) {
	clusterClient := clusterfake.NewSimpleClientset()
	c := &hostingClusterClaimController{
		claimClient:        clusterClient.ClusterV1alpha1().ClusterClaims(),
		hostingClusterName: "cluster-b",
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "hosting-cluster-claim")
	if err := c.sync(context.TODO(), syncContext, factory.DefaultQueueKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	claim, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(context.TODO(), HostingClusterClaimName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected claim to be created: %v", err)
	}
	if claim.Spec.Value != "cluster-b" {
		t.Errorf("expected claim value %q, got %q", "cluster-b", claim.Spec.Value)
	}
}

func TestHostingClusterClaimControllerUpdate(t *testing.T) {
	existing := &clusterv1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{Name: HostingClusterClaimName},
		Spec:       clusterv1alpha1.ClusterClaimSpec{Value: "stale-cluster"},
	}
	clusterClient := clusterfake.NewSimpleClientset(existing)
	c := &hostingClusterClaimController{
		claimClient:        clusterClient.ClusterV1alpha1().ClusterClaims(),
		hostingClusterName: "cluster-b",
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "hosting-cluster-claim")
	if err := c.sync(context.TODO(), syncContext, factory.DefaultQueueKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	claim, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(context.TODO(), HostingClusterClaimName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected claim to still exist: %v", err)
	}
	if claim.Spec.Value != "cluster-b" {
		t.Errorf("expected claim value updated to %q, got %q", "cluster-b", claim.Spec.Value)
	}
}

func TestHostingClusterClaimControllerNoopWhenUnchanged(t *testing.T) {
	existing := &clusterv1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{Name: HostingClusterClaimName},
		Spec:       clusterv1alpha1.ClusterClaimSpec{Value: "cluster-b"},
	}
	clusterClient := clusterfake.NewSimpleClientset(existing)
	c := &hostingClusterClaimController{
		claimClient:        clusterClient.ClusterV1alpha1().ClusterClaims(),
		hostingClusterName: "cluster-b",
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "hosting-cluster-claim")
	if err := c.sync(context.TODO(), syncContext, factory.DefaultQueueKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, action := range clusterClient.Actions() {
		if action.GetVerb() == "update" {
			t.Errorf("expected no update action when the claim value already matches, got %v", action)
		}
	}
}

func TestHostingClusterClaimControllerDeletesWhenEmpty(t *testing.T) {
	existing := &clusterv1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{Name: HostingClusterClaimName},
		Spec:       clusterv1alpha1.ClusterClaimSpec{Value: "cluster-b"},
	}
	clusterClient := clusterfake.NewSimpleClientset(existing)
	c := &hostingClusterClaimController{
		claimClient:        clusterClient.ClusterV1alpha1().ClusterClaims(),
		hostingClusterName: "",
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "hosting-cluster-claim")
	if err := c.sync(context.TODO(), syncContext, factory.DefaultQueueKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(context.TODO(), HostingClusterClaimName, metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Errorf("expected claim to be deleted, got err=%v", err)
	}
}

func TestHostingClusterClaimControllerDeleteNoopWhenAlreadyGone(t *testing.T) {
	clusterClient := clusterfake.NewSimpleClientset()
	c := &hostingClusterClaimController{
		claimClient:        clusterClient.ClusterV1alpha1().ClusterClaims(),
		hostingClusterName: "",
	}

	// Deleting an already-absent claim must not surface as an error.
	syncContext := testingcommon.NewFakeSyncContext(t, "hosting-cluster-claim")
	if err := c.sync(context.TODO(), syncContext, factory.DefaultQueueKey); err != nil {
		t.Fatalf("unexpected error when the claim never existed: %v", err)
	}
}
