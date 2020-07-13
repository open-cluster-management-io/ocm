package managedcluster

import (
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
	}{
		{
			name:            "create a new cluster",
			startingObjects: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedClientConfigs := []clusterv1.ClientConfig{
					{
						URL:      testSpokeExternalServerUrl,
						CABundle: []byte("testcabundle"),
					},
				}
				testinghelpers.AssertActions(t, actions, "get", "create")
				actual := actions[1].(clienttesting.CreateActionImpl).Object
				actualClientConfigs := actual.(*clusterv1.ManagedCluster).Spec.ManagedClusterClientConfigs
				testinghelpers.AssertManagedClusterClientConfigs(t, actualClientConfigs, expectedClientConfigs)
			},
		},
		{
			name:            "create an existed cluster",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			ctrl := managedClusterCreatingController{
				clusterName:             testinghelpers.TestManagedClusterName,
				spokeExternalServerURLs: []string{testSpokeExternalServerUrl},
				spokeCABundle:           []byte("testcabundle"),
				hubClusterClient:        clusterClient,
			}

			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
