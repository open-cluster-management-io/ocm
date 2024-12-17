package lease

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestClockSyncController(t *testing.T) {
	// cases:
	// 1. hub and agent clock is close
	// 2. hub and agent clock is not close
	cases := []struct {
		name            string
		clusters        []runtime.Object
		leases          []runtime.Object
		validateActions func(t *testing.T, leaseActions, clusterActions []clienttesting.Action)
	}{
		{
			name: "hub and agent clock is close",
			clusters: []runtime.Object{
				testinghelpers.NewManagedCluster(),
			},
			leases: []runtime.Object{
				testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(5*time.Second)),
			},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				expected := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionClockSynced,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterClockSynced",
					Message: "The clock of the managed cluster is synced with the hub.",
				}
				testingcommon.AssertActions(t, clusterActions, "patch")
				patch := clusterActions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expected)
			},
		},
		{
			name: "hub and agent clock is not close",
			clusters: []runtime.Object{
				testinghelpers.NewManagedCluster(),
			},
			leases: []runtime.Object{
				testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(301*time.Second)),
			},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				expected := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionClockSynced,
					Status:  metav1.ConditionFalse,
					Reason:  "ManagedClusterClockOutOfSync",
					Message: "The clock of hub and agent is out of sync. This may cause the Unknown status and affect agent functionalities.",
				}
				testingcommon.AssertActions(t, clusterActions, "patch")
				patch := clusterActions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expected)
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

			leaseClient := kubefake.NewSimpleClientset(c.leases...)
			leaseInformerFactory := kubeinformers.NewSharedInformerFactory(leaseClient, time.Minute*10)
			leaseStore := leaseInformerFactory.Coordination().V1().Leases().Informer().GetStore()
			for _, lease := range c.leases {
				if err := leaseStore.Add(lease); err != nil {
					t.Fatal(err)
				}
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName)

			controller := &clockSyncController{
				patcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseLister:   leaseInformerFactory.Coordination().V1().Leases().Lister(),
				eventRecorder: syncCtx.Recorder(),
			}
			syncErr := controller.sync(context.TODO(), syncCtx)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}
			c.validateActions(t, leaseClient.Actions(), clusterClient.Actions())
		})
	}

}
