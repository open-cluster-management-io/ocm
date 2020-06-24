package managedcluster

import (
	"context"
	"reflect"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/helpers"

	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubeversion "k8s.io/client-go/pkg/version"
	clienttesting "k8s.io/client-go/testing"
)

const testManagedClusterName = "testmanagedcluster"

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
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected 0 call but got: %#v", actions)
				}
			},
			expectedErr: "unable to get managed cluster with name \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:            "sync an unaccepted managed cluster",
			startingObjects: []runtime.Object{newManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected 0 call but got: %#v", actions)
				}
			},
		},
		{
			name:            "sync an accepted managed cluster",
			startingObjects: []runtime.Object{newAcceptedManagedCluster()},
			nodes:           []runtime.Object{newNode("testnode1", newResourceList(32, 64), newResourceList(16, 32))},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				expectedCondition := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterJoined",
					Message: "Managed cluster joined",
				}
				assertCondition(t, actual, expectedCondition)
				assertStatusVersion(t, actual, kubeversion.Get())
				assertStatusResource(t, actual, newResourceList(32, 64), newResourceList(16, 32))
			},
		},
		{
			name:            "sync a joined managed cluster without status change",
			startingObjects: []runtime.Object{newJoinedManagedCluster(newResourceList(32, 64), newResourceList(16, 32))},
			nodes:           []runtime.Object{newNode("testnode1", newResourceList(32, 64), newResourceList(16, 32))},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get")
			},
		},
		{
			name:            "sync a joined managed cluster with status change",
			startingObjects: []runtime.Object{newJoinedManagedCluster(newResourceList(32, 64), newResourceList(16, 32))},
			nodes: []runtime.Object{
				newNode("testnode1", newResourceList(32, 64), newResourceList(16, 32)),
				newNode("testnode2", newResourceList(32, 64), newResourceList(16, 32)),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				expectedCondition := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterJoined",
					Message: "Managed cluster joined",
				}
				assertCondition(t, actual, expectedCondition)
				assertStatusVersion(t, actual, kubeversion.Get())
				assertStatusResource(t, actual, newResourceList(64, 128), newResourceList(32, 64))
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
				clusterName:      testManagedClusterName,
				hubClusterClient: clusterClient,
				hubClusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				discoveryClient:  kubeClient.Discovery(),
				nodeLister:       kubeInformerFactory.Core().V1().Nodes().Lister(),
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

			c.validateActions(t, clusterClient.Actions())
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

func assertManagedCluster(t *testing.T, actual runtime.Object, expectedName string) {
	managedCluster, ok := actual.(*clusterv1.ManagedCluster)
	if !ok {
		t.Errorf("expected managed cluster but got: %#v", actual)
	}
	if managedCluster.Name != expectedName {
		t.Errorf("expected %s but got: %#v", expectedName, managedCluster.Name)
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

func assertStatusVersion(t *testing.T, actual runtime.Object, expected version.Info) {
	managedCluster := actual.(*clusterv1.ManagedCluster)
	if !reflect.DeepEqual(managedCluster.Status.Version, clusterv1.ManagedClusterVersion{
		Kubernetes: expected.GitVersion,
	}) {
		t.Errorf("expected %s but got: %#v", expected, managedCluster.Status.Version)
	}
}

func assertStatusResource(t *testing.T, actual runtime.Object, expectedCapacity, expectedAllocatable corev1.ResourceList) {
	managedCluster := actual.(*clusterv1.ManagedCluster)
	if !reflect.DeepEqual(managedCluster.Status.Capacity["cpu"], expectedCapacity["cpu"]) {
		t.Errorf("expected %#v but got: %#v", expectedCapacity, managedCluster.Status.Capacity)
	}
	if !reflect.DeepEqual(managedCluster.Status.Capacity["memory"], expectedCapacity["memory"]) {
		t.Errorf("expected %#v but got: %#v", expectedCapacity, managedCluster.Status.Capacity)
	}
	if !reflect.DeepEqual(managedCluster.Status.Allocatable["cpu"], expectedAllocatable["cpu"]) {
		t.Errorf("expected %#v but got: %#v", expectedAllocatable, managedCluster.Status.Allocatable)
	}
	if !reflect.DeepEqual(managedCluster.Status.Allocatable["memory"], expectedAllocatable["memory"]) {
		t.Errorf("expected %#v but got: %#v", expectedAllocatable, managedCluster.Status.Allocatable)
	}
}

func newManagedCluster(conditions ...clusterv1.StatusCondition) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: testManagedClusterName,
		},
		Status: clusterv1.ManagedClusterStatus{
			Conditions: conditions,
		},
	}
}

func newAcceptedManagedCluster() *clusterv1.ManagedCluster {
	return newManagedCluster(clusterv1.StatusCondition{
		Type:    clusterv1.ManagedClusterConditionHubAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  "HubClusterAdminAccepted",
		Message: "Accepted by hub cluster admin",
	})
}

func newJoinedManagedCluster(capacity, allocatable corev1.ResourceList) *clusterv1.ManagedCluster {
	managedCluster := newManagedCluster(
		clusterv1.StatusCondition{
			Type:    clusterv1.ManagedClusterConditionHubAccepted,
			Status:  metav1.ConditionTrue,
			Reason:  "HubClusterAdminAccepted",
			Message: "Accepted by hub cluster admin",
		},
		clusterv1.StatusCondition{
			Type:    clusterv1.ManagedClusterConditionJoined,
			Status:  metav1.ConditionTrue,
			Reason:  "ManagedClusterJoined",
			Message: "Managed cluster joined",
		},
	)
	managedCluster.Status.Capacity = clusterv1.ResourceList{
		"cpu":    capacity.Cpu().DeepCopy(),
		"memory": capacity.Memory().DeepCopy(),
	}
	managedCluster.Status.Allocatable = clusterv1.ResourceList{
		"cpu":    allocatable.Cpu().DeepCopy(),
		"memory": allocatable.Memory().DeepCopy(),
	}
	managedCluster.Status.Version = clusterv1.ManagedClusterVersion{
		Kubernetes: kubeversion.Get().GitVersion,
	}
	return managedCluster
}

func newResourceList(cpu, mem int) corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalExponent),
		corev1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*mem), resource.BinarySI),
	}
}

func newNode(name string, capacity, allocatable corev1.ResourceList) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NodeStatus{
			Capacity:    capacity,
			Allocatable: allocatable,
		},
	}
}
