package registration

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	testingcommon "open-cluster-management.io/sdk-go/pkg/testing"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
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
				testingcommon.AssertActions(t, actions, "get", "create")
				actual := actions[1].(clienttesting.CreateActionImpl).Object
				actualClientConfigs := actual.(*clusterv1.ManagedCluster).Spec.ManagedClusterClientConfigs
				testinghelpers.AssertManagedClusterClientConfigs(t, actualClientConfigs, expectedClientConfigs)
				clusterannotations := actual.(*clusterv1.ManagedCluster).Annotations
				if len(clusterannotations) != 1 {
					t.Errorf("expected cluster annotations %#v but got: %#v", 1, len(clusterannotations))
				}
				if value, ok := clusterannotations["agent.open-cluster-management.io/test"]; !ok || value != "true" {
					t.Errorf("expected cluster annotations %#v but got: %#v", "agent.open-cluster-management.io/test",
						clusterannotations["agent.open-cluster-management.io/test"])
				}
			},
		},
		{
			name:            "create an existed cluster",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
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
				clusterAnnotations: map[string]string{
					"agent.open-cluster-management.io/test": "true",
				},
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, ""))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
