package managedcluster

import (
	"context"
	"reflect"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

func TestSync(t *testing.T) {
	cases := []struct {
		name            string
		cluster         runtime.Object
		claims          []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:            "sync no managed cluster",
			validateActions: testinghelpers.AssertNoActions,
			expectedErr:     "unable to get managed cluster with name \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:            "skip when managed cluster does not join the hub yet",
			cluster:         testinghelpers.NewManagedCluster(),
			validateActions: testinghelpers.AssertNoActions,
		},
		{
			name:    "sync a joined managed cluster",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			claims: []runtime.Object{
				&clusterv1alpha1.ClusterClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "b",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "update")
				cluster := actions[1].(clienttesting.UpdateActionImpl).Object
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "a",
						Value: "b",
					},
				}
				actual := cluster.(*clusterv1.ManagedCluster).Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.cluster != nil {
				objects = append(objects, c.cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			if c.cluster != nil {
				clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(c.cluster)
			}

			for _, claim := range c.claims {
				clusterInformerFactory.Cluster().V1alpha1().ClusterClaims().Informer().GetStore().Add(claim)
			}

			ctrl := managedClusterClaimController{
				clusterName:      testinghelpers.TestManagedClusterName,
				maxClusterClaims: 20,
				hubClusterClient: clusterClient,
				hubClusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				claimLister:      clusterInformerFactory.Cluster().V1alpha1().ClusterClaims().Lister(),
			}

			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			testinghelpers.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestExposeClaims(t *testing.T) {
	cases := []struct {
		name             string
		cluster          *clusterv1.ManagedCluster
		claims           []*clusterv1alpha1.ClusterClaim
		maxClusterClaims int
		validateActions  func(t *testing.T, actions []clienttesting.Action)
		expectedErr      string
	}{
		{
			name:    "sync claims into status of the managed cluster",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			claims: []*clusterv1alpha1.ClusterClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "b",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "update")
				cluster := actions[1].(clienttesting.UpdateActionImpl).Object
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "a",
						Value: "b",
					},
				}
				actual := cluster.(*clusterv1.ManagedCluster).Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
		{
			name:    "truncate cluster claims",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			claims: []*clusterv1alpha1.ClusterClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "b",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "e",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "f",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "id.k8s.io",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "cluster1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "d",
					},
				},
			},
			maxClusterClaims: 3,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "update")
				cluster := actions[1].(clienttesting.UpdateActionImpl).Object
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "id.k8s.io",
						Value: "cluster1",
					},
					{
						Name:  "a",
						Value: "b",
					},
					{
						Name:  "c",
						Value: "d",
					},
				}
				actual := cluster.(*clusterv1.ManagedCluster).Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
		{
			name: "remove claims from managed cluster",
			cluster: newManagedCluster([]clusterv1.ManagedClusterClaim{
				{
					Name:  "a",
					Value: "b",
				},
			}),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "update")
				cluster := actions[1].(clienttesting.UpdateActionImpl).Object
				actual := cluster.(*clusterv1.ManagedCluster).Status.ClusterClaims
				if len(actual) > 0 {
					t.Errorf("expected no cluster claim but got: %v", actual)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.cluster != nil {
				objects = append(objects, c.cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			if c.cluster != nil {
				clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(c.cluster)
			}

			for _, claim := range c.claims {
				clusterInformerFactory.Cluster().V1alpha1().ClusterClaims().Informer().GetStore().Add(claim)
			}

			if c.maxClusterClaims == 0 {
				c.maxClusterClaims = 20
			}

			ctrl := managedClusterClaimController{
				clusterName:      testinghelpers.TestManagedClusterName,
				maxClusterClaims: c.maxClusterClaims,
				hubClusterClient: clusterClient,
				hubClusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				claimLister:      clusterInformerFactory.Cluster().V1alpha1().ClusterClaims().Lister(),
			}

			syncErr := ctrl.exposeClaims(context.TODO(), testinghelpers.NewFakeSyncContext(t, c.cluster.Name), c.cluster)
			testinghelpers.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func newManagedCluster(claims []clusterv1.ManagedClusterClaim) *clusterv1.ManagedCluster {
	cluster := testinghelpers.NewJoinedManagedCluster()
	cluster.Status.ClusterClaims = claims
	return cluster
}
