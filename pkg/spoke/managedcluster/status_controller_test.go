package managedcluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	discovery "k8s.io/client-go/discovery"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
)

type serverResponse struct {
	httpStatus  int
	responseMsg string
}

func TestHealthCheck(t *testing.T) {
	serverResponse := &serverResponse{}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if req.URL.Path == "/version" {
			output, err := json.Marshal(version.Info{
				GitVersion: "test-version",
			})
			if err != nil {
				t.Errorf("unexpected encoding error: %v", err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(output); err != nil {
				t.Fatal(err)
			}
			return
		}

		w.WriteHeader(serverResponse.httpStatus)
		if _, err := w.Write([]byte(serverResponse.responseMsg)); err != nil {
			t.Fatal(err)
		}
	}))
	defer apiServer.Close()

	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(&rest.Config{Host: apiServer.URL})

	cases := []struct {
		name            string
		clusters        []runtime.Object
		nodes           []runtime.Object
		httpStatus      int
		responseMsg     string
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:            "there are no managed clusters",
			clusters:        []runtime.Object{},
			validateActions: testinghelpers.AssertNoActions,
			expectedErr:     "unable to get managed cluster \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:        "kube-apiserver is not health",
			clusters:    []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			httpStatus:  http.StatusInternalServerError,
			responseMsg: "internal server error",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  "ManagedClusterKubeAPIServerUnavailable",
					Message: "The kube-apiserver is not ok, status code: 500, an error on the server (\"internal server error\") has prevented the request from succeeding",
				}
				testinghelpers.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
		{
			name:     "kube-apiserver is ok",
			clusters: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			nodes: []runtime.Object{
				testinghelpers.NewNode("testnode1", testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
			},
			httpStatus: http.StatusOK,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterAvailable",
					Message: "Managed cluster is available",
				}
				expectedStatus := clusterv1.ManagedClusterStatus{
					Version: clusterv1.ManagedClusterVersion{
						Kubernetes: "test-version",
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
				testinghelpers.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
				testinghelpers.AssertManagedClusterStatus(t, managedCluster.Status, expectedStatus)
			},
		},
		{
			name:       "there is no livez endpoint",
			clusters:   []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			nodes:      []runtime.Object{},
			httpStatus: http.StatusNotFound,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterAvailable",
					Message: "Managed cluster is available",
				}
				testinghelpers.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
		{
			name:       "livez is forbidden",
			clusters:   []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			nodes:      []runtime.Object{},
			httpStatus: http.StatusForbidden,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterAvailable",
					Message: "Managed cluster is available",
				}
				testinghelpers.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
		{
			name: "merge managed cluster status",
			clusters: []runtime.Object{
				testinghelpers.NewManagedClusterWithStatus(
					corev1.ResourceList{
						"sockets": *resource.NewQuantity(int64(1200), resource.DecimalExponent),
						"cores":   *resource.NewQuantity(int64(128), resource.DecimalExponent),
					},
					testinghelpers.NewResourceList(16, 32)),
			},
			nodes: []runtime.Object{
				testinghelpers.NewNode("testnode1", testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
				testinghelpers.NewNode("testnode2", testinghelpers.NewResourceList(32, 64), testinghelpers.NewResourceList(16, 32)),
			},
			httpStatus: http.StatusOK,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterJoined",
					Message: "Managed cluster joined",
				}
				expectedStatus := clusterv1.ManagedClusterStatus{
					Version: clusterv1.ManagedClusterVersion{
						Kubernetes: "test-version",
					},
					Capacity: clusterv1.ResourceList{
						"sockets":                *resource.NewQuantity(int64(1200), resource.DecimalExponent),
						"cores":                  *resource.NewQuantity(int64(128), resource.DecimalExponent),
						clusterv1.ResourceCPU:    *resource.NewQuantity(int64(64), resource.DecimalExponent),
						clusterv1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*128), resource.BinarySI),
					},
					Allocatable: clusterv1.ResourceList{
						clusterv1.ResourceCPU:    *resource.NewQuantity(int64(32), resource.DecimalExponent),
						clusterv1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*64), resource.BinarySI),
					},
				}
				testinghelpers.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
				testinghelpers.AssertManagedClusterStatus(t, managedCluster.Status, expectedStatus)
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

			kubeClient := kubefake.NewSimpleClientset(c.nodes...)
			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)
			nodeStore := kubeInformerFactory.Core().V1().Nodes().Informer().GetStore()
			for _, node := range c.nodes {
				if err := nodeStore.Add(node); err != nil {
					t.Fatal(err)
				}
			}

			serverResponse.httpStatus = c.httpStatus
			serverResponse.responseMsg = c.responseMsg

			ctrl := &managedClusterStatusController{
				clusterName:                   testinghelpers.TestManagedClusterName,
				hubClusterClient:              clusterClient,
				hubClusterLister:              clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				managedClusterDiscoveryClient: discoveryClient,
				nodeLister:                    kubeInformerFactory.Core().V1().Nodes().Lister(),
			}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			testinghelpers.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
