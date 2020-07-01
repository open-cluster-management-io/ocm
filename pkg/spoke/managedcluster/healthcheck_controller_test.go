package managedcluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	discovery "k8s.io/client-go/discovery"
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
		w.WriteHeader(serverResponse.httpStatus)
		w.Write([]byte(serverResponse.responseMsg))
	}))
	defer apiServer.Close()

	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(&rest.Config{Host: apiServer.URL})

	cases := []struct {
		name            string
		clusters        []runtime.Object
		httpStatus      int
		responseMsg     string
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:     "there are no managed clusters",
			clusters: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions)
			},
			expectedErr: "unable to get managed cluster \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:        "kube-apiserver is not health",
			clusters:    []runtime.Object{newAcceptedManagedCluster()},
			httpStatus:  http.StatusInternalServerError,
			responseMsg: "internal server error",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				expectedCondition := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  "ManagedClusterKubeAPIServerUnavailable",
					Message: "The kube-apiserver is not ok: an error on the server (\"internal server error\") has prevented the request from succeeding",
				}
				assertCondition(t, actual, expectedCondition)
			},
		},
		{
			name:       "kube-apiserver is ok",
			clusters:   []runtime.Object{newAcceptedManagedCluster()},
			httpStatus: http.StatusOK,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				expectedCondition := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterAvailable",
					Message: "Managed cluster is available",
				}
				assertCondition(t, actual, expectedCondition)
			},
		},
		{
			name:       "there is no readyz endpoint",
			clusters:   []runtime.Object{newAcceptedManagedCluster()},
			httpStatus: http.StatusNotFound,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				expectedCondition := clusterv1.StatusCondition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterAvailable",
					Message: "Managed cluster is available",
				}
				assertCondition(t, actual, expectedCondition)
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

			serverResponse.httpStatus = c.httpStatus
			serverResponse.responseMsg = c.responseMsg

			ctrl := &managedClusterHealthCheckController{
				clusterName:                   testManagedClusterName,
				hubClusterClient:              clusterClient,
				hubClusterLister:              clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				managedClusterDiscoveryClient: discoveryClient,
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
