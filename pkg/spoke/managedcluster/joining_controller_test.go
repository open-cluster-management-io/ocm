package managedcluster

import (
	"context"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubeversion "k8s.io/client-go/pkg/version"
	clienttesting "k8s.io/client-go/testing"
)

func TestSyncManagedCluster(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		nodes           []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:            "sync no managed cluster",
			startingObjects: []runtime.Object{},
			validateActions: testinghelpers.AssertNoActions,
			expectedErr:     "unable to get managed cluster with name \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:            "sync an unaccepted managed cluster",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: testinghelpers.AssertNoActions,
		},
		{
			name:            "sync an accepted managed cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			nodes: []runtime.Object{
				testinghelpers.NewNode("testnode1", testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterJoined",
					Message: "Managed cluster joined",
				}
				expectedStatus := clusterv1.ManagedClusterStatus{
					Version: clusterv1.ManagedClusterVersion{
						Kubernetes: kubeversion.Get().GitVersion,
					},
					Capacity: clusterv1.ResourceList{
						clusterv1.ResourceCPU:    *resource.NewQuantity(int64(32), resource.DecimalExponent),
						clusterv1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*64), resource.BinarySI),
					},
					Allocatable: clusterv1.ResourceList{
						clusterv1.ResourceCPU:    *resource.NewQuantity(int64(16), resource.DecimalExponent),
						clusterv1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*32), resource.BinarySI),
					},
				}
				testinghelpers.AssertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				testinghelpers.AssertManagedClusterCondition(t, actual.(*clusterv1.ManagedCluster).Status.Conditions, expectedCondition)
				testinghelpers.AssertManagedClusterStatus(t, actual.(*clusterv1.ManagedCluster).Status, expectedStatus)
			},
		},
		{
			name: "sync a joined managed cluster without status change",
			startingObjects: []runtime.Object{
				testinghelpers.NewManagedClusterWithStatus(testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
			},
			nodes: []runtime.Object{
				testinghelpers.NewNode("testnode1", testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get")
			},
		},
		{
			name:            "sync a joined managed cluster with status change",
			startingObjects: []runtime.Object{testinghelpers.NewJoinedManagedCluster()},
			nodes: []runtime.Object{
				testinghelpers.NewNode("testnode1", testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
				testinghelpers.NewNode("testnode2", testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterJoined",
					Message: "Managed cluster joined",
				}
				expectedStatus := clusterv1.ManagedClusterStatus{
					Version: clusterv1.ManagedClusterVersion{
						Kubernetes: kubeversion.Get().GitVersion,
					},
					Capacity: clusterv1.ResourceList{
						clusterv1.ResourceCPU:    *resource.NewQuantity(int64(64), resource.DecimalExponent),
						clusterv1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*128), resource.BinarySI),
					},
					Allocatable: clusterv1.ResourceList{
						clusterv1.ResourceCPU:    *resource.NewQuantity(int64(32), resource.DecimalExponent),
						clusterv1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*64), resource.BinarySI),
					},
				}
				testinghelpers.AssertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				testinghelpers.AssertManagedClusterCondition(t, actual.(*clusterv1.ManagedCluster).Status.Conditions, expectedCondition)
				testinghelpers.AssertManagedClusterStatus(t, actual.(*clusterv1.ManagedCluster).Status, expectedStatus)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.startingObjects {
				clusterStore.Add(cluster)
			}

			kubeClient := kubefake.NewSimpleClientset(c.nodes...)
			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)
			nodeStore := kubeInformerFactory.Core().V1().Nodes().Informer().GetStore()
			for _, node := range c.nodes {
				nodeStore.Add(node)
			}

			ctrl := managedClusterJoiningController{
				clusterName:      testinghelpers.TestManagedClusterName,
				hubClusterClient: clusterClient,
				hubClusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				discoveryClient:  kubeClient.Discovery(),
				nodeLister:       kubeInformerFactory.Core().V1().Nodes().Lister(),
			}

			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			testinghelpers.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
