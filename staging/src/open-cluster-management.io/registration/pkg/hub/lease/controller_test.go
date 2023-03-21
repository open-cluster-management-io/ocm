package lease

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"

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
	}{
		{
			name:          "sync unaccepted managed cluster",
			clusters:      []runtime.Object{testinghelpers.NewManagedCluster()},
			clusterLeases: []runtime.Object{},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, leaseActions)
				testinghelpers.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:          "there is no lease for a managed cluster",
			clusters:      []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			clusterLeases: []runtime.Object{},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testinghelpers.AssertActions(t, leaseActions, "create")
				testinghelpers.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:     "managed cluster stop update lease",
			clusters: []runtime.Object{testinghelpers.NewAvailableManagedCluster()},
			clusterLeases: []runtime.Object{
				testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-5*time.Minute)),
				testinghelpers.NewManagedClusterLease(fmt.Sprintf("cluster-lease-%s", testinghelpers.TestManagedClusterName), now.Add(-5*time.Minute)),
			},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				expected := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionUnknown,
					Reason:  "ManagedClusterLeaseUpdateStopped",
					Message: "Registration agent stopped updating its lease.",
				}
				testinghelpers.AssertActions(t, clusterActions, "get", "patch")
				patch := clusterActions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertCondition(t, managedCluster.Status.Conditions, expected)
			},
		},
		{
			name:          "managed cluster is available",
			clusters:      []runtime.Object{testinghelpers.NewAvailableManagedCluster()},
			clusterLeases: []runtime.Object{testinghelpers.NewManagedClusterLease("managed-cluster-lease", now)},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:     "managed cluster is deleting",
			clusters: []runtime.Object{newDeletingManagedCluster()},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				expected := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionUnknown,
					Reason:  "ManagedClusterLeaseUpdateStopped",
					Message: "Registration agent stopped updating its lease.",
				}
				testinghelpers.AssertActions(t, clusterActions, "get", "patch")
				patch := clusterActions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertCondition(t, managedCluster.Status.Conditions, expected)
			},
		},
		{
			name:          "managed cluster is unknown",
			clusters:      []runtime.Object{testinghelpers.NewUnknownManagedCluster()},
			clusterLeases: []runtime.Object{testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-5*time.Minute))},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, clusterActions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			leaseClient := kubefake.NewSimpleClientset(c.clusterLeases...)
			leaseInformerFactory := kubeinformers.NewSharedInformerFactory(leaseClient, time.Minute*10)
			leaseStore := leaseInformerFactory.Coordination().V1().Leases().Informer().GetStore()
			for _, lease := range c.clusterLeases {
				if err := leaseStore.Add(lease); err != nil {
					t.Fatal(err)
				}
			}

			syncCtx := testinghelpers.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName)

			ctrl := &leaseController{
				kubeClient:    leaseClient,
				clusterClient: clusterClient,
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseLister:   leaseInformerFactory.Coordination().V1().Leases().Lister(),
				eventRecorder: syncCtx.Recorder(),
			}
			syncErr := ctrl.sync(context.TODO(), syncCtx)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}
			c.validateActions(t, leaseClient.Actions(), clusterClient.Actions())
		})
	}
}

func newDeletingManagedCluster() *clusterv1.ManagedCluster {
	now := metav1.Now()
	cluster := testinghelpers.NewAcceptedManagedCluster()
	cluster.DeletionTimestamp = &now
	return cluster
}
