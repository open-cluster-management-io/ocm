package lease

import (
	"context"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/helpers"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	coordv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

var now = time.Now()

func TestSync(t *testing.T) {
	cases := []struct {
		name            string
		clusters        []runtime.Object
		clusterLeases   []runtime.Object
		validateActions func(t *testing.T, leaseActions, clusterActions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:          "sync unaccepted managed cluster",
			clusters:      []runtime.Object{newManagedCluster()},
			clusterLeases: []runtime.Object{},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				assertActions(t, leaseActions)
				assertActions(t, clusterActions)
			},
		},
		{
			name:          "there is no lease for a managed cluster",
			clusters:      []runtime.Object{newManagedCluster(newAcceptedCondtion())},
			clusterLeases: []runtime.Object{},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				assertActions(t, leaseActions, "create")
				assertActions(t, clusterActions)
			},
		},
		{
			name:          "managed cluster stop update lease",
			clusters:      []runtime.Object{newManagedCluster(newAcceptedCondtion(), newAvailableCondtion())},
			clusterLeases: []runtime.Object{newClusterLease(now.Add(-5 * time.Minute))},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				assertActions(t, clusterActions, "get", "update")
				actual := clusterActions[1].(clienttesting.UpdateActionImpl).Object
				expected := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionUnknown,
					Reason:  "ManagedClusterLeaseUpdateStopped",
					Message: "Registration agent stopped updating its lease within 5 minutes.",
				}
				assertCondition(t, actual, expected)
			},
		},
		{
			name:          "managed cluster is available",
			clusters:      []runtime.Object{newManagedCluster(newAcceptedCondtion(), newAvailableCondtion())},
			clusterLeases: []runtime.Object{newClusterLease(now)},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				assertActions(t, clusterActions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				clusterStore.Add(cluster)
			}

			leaseClient := kubefake.NewSimpleClientset(c.clusterLeases...)
			leaseInformerFactory := kubeinformers.NewSharedInformerFactory(leaseClient, time.Minute*10)
			leaseStore := leaseInformerFactory.Coordination().V1().Leases().Informer().GetStore()
			for _, lease := range c.clusterLeases {
				leaseStore.Add(lease)
			}

			ctrl := &leaseController{
				kubeClient:    leaseClient,
				clusterClient: clusterClient,
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseLister:   leaseInformerFactory.Coordination().V1().Leases().Lister(),
			}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			if len(c.expectedErr) > 0 && syncErr == nil {
				t.Errorf("expected %q error", c.expectedErr)
				return
			}
			if len(c.expectedErr) > 0 && syncErr != nil && syncErr.Error() != c.expectedErr {
				t.Errorf("expected %q error, got %q", c.expectedErr, syncErr.Error())
				return
			}
			if len(c.expectedErr) == 0 && syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}
			c.validateActions(t, leaseClient.Actions(), clusterClient.Actions())
		})
	}
}

func assertActions(t *testing.T, actualActions []clienttesting.Action, expectedActions ...string) {
	if len(actualActions) != len(expectedActions) {
		t.Errorf("expected %d call but got: %#v", len(expectedActions), actualActions)
	}
	for i, expected := range expectedActions {
		if actualActions[i].GetVerb() != expected {
			t.Errorf("expected %s action but got: %#v", expected, actualActions[i])
		}
	}
}

func assertCondition(t *testing.T, actual runtime.Object, expectedCondition clusterv1.StatusCondition) {
	managedCluster := actual.(*clusterv1.ManagedCluster)
	cond := helpers.FindManagedClusterCondition(managedCluster.Status.Conditions, expectedCondition.Type)
	if cond == nil {
		t.Errorf("expected condition %s but got: %s", expectedCondition.Type, cond.Type)
	}
	if cond.Status != expectedCondition.Status {
		t.Errorf("expected status %s but got: %s", expectedCondition.Status, cond.Status)
	}
	if cond.Reason != expectedCondition.Reason {
		t.Errorf("expected reason %s but got: %s", expectedCondition.Reason, cond.Reason)
	}
	if cond.Message != expectedCondition.Message {
		t.Errorf("expected message %s but got: %s", expectedCondition.Message, cond.Message)
	}
}

func newManagedCluster(conditions ...clusterv1.StatusCondition) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testmanagedcluster",
		},
		Status: clusterv1.ManagedClusterStatus{
			Conditions: conditions,
		},
	}
}

func newAcceptedCondtion() clusterv1.StatusCondition {
	return clusterv1.StatusCondition{
		Type:    clusterv1.ManagedClusterConditionHubAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  "HubClusterAdminAccepted",
		Message: "Accepted by hub cluster admin",
	}
}

func newAvailableCondtion() clusterv1.StatusCondition {
	return clusterv1.StatusCondition{
		Type:    clusterv1.ManagedClusterConditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedClusterAvailable",
		Message: "Managed cluster is available",
	}
}

func newClusterLease(renewTime time.Time) *coordv1.Lease {
	return &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-lease-testmanagedcluster",
			Namespace: "testmanagedcluster",
		},
		Spec: coordv1.LeaseSpec{
			RenewTime: &metav1.MicroTime{Time: renewTime},
		},
	}
}
