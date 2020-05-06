package spokecluster

import (
	"bytes"
	"context"
	"testing"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

const testSpokeExternalServerUrl = "https://192.168.3.77:32769"

func TestCreateSpokeCluster(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:            "create a new cluster",
			startingObjects: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "create")
				actual := actions[1].(clienttesting.CreateActionImpl).Object
				assertSpokeExternalServerUrl(t, actual, testSpokeExternalServerUrl)
				assertSpokeCABundle(t, actual, []byte("testcabundle"))
			},
		},
		{
			name:            "create an existed cluster",
			startingObjects: []runtime.Object{newSpokeCluster([]clusterv1.StatusCondition{})},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			ctrl := spokeClusterCreatingController{
				clusterName:            testSpokeClusterName,
				spokeExternalServerUrl: testSpokeExternalServerUrl,
				spokeCABundle:          []byte("testcabundle"),
				hubClusterClient:       clusterClient,
			}

			syncErr := ctrl.sync(context.TODO(), newFakeSyncContext(t))
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

func assertSpokeExternalServerUrl(t *testing.T, actual runtime.Object, expected string) {
	spokeCluster := actual.(*clusterv1.SpokeCluster)
	if spokeCluster.Spec.SpokeClientConfig.URL != expected {
		t.Errorf("expected %q error, but got %q", expected, spokeCluster.Spec.SpokeClientConfig.URL)
	}
}

func assertSpokeCABundle(t *testing.T, actual runtime.Object, expected []byte) {
	spokeCluster := actual.(*clusterv1.SpokeCluster)
	if !bytes.Equal(spokeCluster.Spec.SpokeClientConfig.CABundle, expected) {
		t.Errorf("expected %q error, but got %q", expected, spokeCluster.Spec.SpokeClientConfig.CABundle)
	}
}
