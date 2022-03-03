package managedclusterset

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestSyncClusterSetLabel(t *testing.T) {
	cases := []struct {
		name             string
		existingClusters []*clusterv1.ManagedCluster
		validateActions  func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:             "sync a deleted cluster",
			existingClusters: []*clusterv1.ManagedCluster{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name: "sync a deleting cluster",
			existingClusters: []*clusterv1.ManagedCluster{
				newCluster(testinghelpers.TestManagedClusterName, true),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name: "sync a labeled cluster",
			existingClusters: []*clusterv1.ManagedCluster{
				newClusterWithLabel(testinghelpers.TestManagedClusterName, clusterSetLabel, defaultManagedClusterSetValue),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name: "sync a cluster",
			existingClusters: []*clusterv1.ManagedCluster{
				newCluster(testinghelpers.TestManagedClusterName, false),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
				cluster := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1.ManagedCluster)
				if !hasLabel(cluster, clusterSetLabel, defaultManagedClusterSetValue) {
					t.Errorf("expected label %v:%v is not found", clusterSetLabel, defaultManagedClusterSetValue)
				}

			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			for _, cluster := range c.existingClusters {
				objects = append(objects, cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)
			for _, cluster := range c.existingClusters {
				informerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster)
			}

			ctrl := defaultManagedClusterSetLabelController{
				clusterClient: clusterClient,
				clusterLister: informerFactory.Cluster().V1().ManagedClusters().Lister(),
				eventRecorder: eventstesting.NewTestingEventRecorder(t),
			}

			syncErr := ctrl.sync(context.Background(), testinghelpers.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())

		})
	}
}

func newCluster(name string, terminating bool) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if terminating {
		now := metav1.Now()
		cluster.DeletionTimestamp = &now
	}

	return cluster
}

func newClusterWithLabel(name string, labelKey string, labelValue string) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				labelKey: labelValue,
			},
		},
	}
	return cluster
}

func hasLabel(mcl *clusterv1.ManagedCluster, key string, value string) bool {
	v, ok := mcl.Labels[key]
	return ok && (v == value)
}
