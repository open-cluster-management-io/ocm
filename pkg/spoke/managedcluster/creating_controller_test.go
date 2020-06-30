package managedcluster

import (
	"bytes"
	"context"
	"testing"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
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
			startingObjects: []runtime.Object{newManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			ctrl := managedClusterCreatingController{
				clusterName:             testManagedClusterName,
				spokeExternalServerURLs: []string{testSpokeExternalServerUrl},
				spokeCABundle:           []byte("testcabundle"),
				hubClusterClient:        clusterClient,
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

func assertSpokeExternalServerUrl(t *testing.T, actual runtime.Object, expected string) {
	spokeCluster := actual.(*clusterv1.ManagedCluster)
	if len(spokeCluster.Spec.ManagedClusterClientConfigs) != 1 {
		t.Errorf("expected one spoke client config, but got %v", spokeCluster.Spec.ManagedClusterClientConfigs)
	}
	if spokeCluster.Spec.ManagedClusterClientConfigs[0].URL != expected {
		t.Errorf("expected %q error, but got %q", expected, spokeCluster.Spec.ManagedClusterClientConfigs[0].URL)
	}
}

func assertSpokeCABundle(t *testing.T, actual runtime.Object, expected []byte) {
	spokeCluster := actual.(*clusterv1.ManagedCluster)
	if len(spokeCluster.Spec.ManagedClusterClientConfigs) != 1 {
		t.Errorf("expected one spoke client config, but got %v", spokeCluster.Spec.ManagedClusterClientConfigs)
	}
	if !bytes.Equal(spokeCluster.Spec.ManagedClusterClientConfigs[0].CABundle, expected) {
		t.Errorf("expected %q error, but got %q", expected, spokeCluster.Spec.ManagedClusterClientConfigs[0].CABundle)
	}
}
