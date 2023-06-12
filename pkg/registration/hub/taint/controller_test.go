package taint

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/common/patcher"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSyncTaintCluster(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "ManagedClusterConditionAvailable conditionStatus is True",
			startingObjects: []runtime.Object{testinghelpers.NewAvailableManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:            "ManagedClusterConditionAvailable conditionStatus is False",
			startingObjects: []runtime.Object{testinghelpers.NewUnAvailableManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patchData, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				taints := []v1.Taint{UnavailableTaint}
				if !reflect.DeepEqual(managedCluster.Spec.Taints, taints) {
					t.Errorf("expected taint %#v, but actualTaints: %#v", taints, managedCluster.Spec.Taints)
				}
			},
		},
		{
			name:            "There is no ManagedClusterConditionAvailable",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patchData, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				taints := []v1.Taint{UnreachableTaint}
				if !reflect.DeepEqual(managedCluster.Spec.Taints, taints) {
					t.Errorf("expected taint %#v, but actualTaints: %#v", taints, managedCluster.Spec.Taints)
				}
			},
		},
		{
			name:            "ManagedClusterConditionAvailable conditionStatus is Unknown",
			startingObjects: []runtime.Object{testinghelpers.NewUnknownManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patchData, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				taints := []v1.Taint{UnreachableTaint}
				if !reflect.DeepEqual(managedCluster.Spec.Taints, taints) {
					t.Errorf("expected taint %#v, but actualTaints: %#v", taints, managedCluster.Spec.Taints)
				}
			},
		},
		{
			name:            "sync a deleted spoke cluster",
			startingObjects: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.startingObjects {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := taintController{
				patcher.NewPatcher[
					*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(), eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
