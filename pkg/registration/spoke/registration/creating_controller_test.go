package registration

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
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
				clusterName: testinghelpers.TestManagedClusterName,
				clusterDecorators: []ManagedClusterDecorator{
					AnnotationDecorator(map[string]string{
						"agent.open-cluster-management.io/test": "true",
					}),
					ClientConfigDecorator([]string{testSpokeExternalServerUrl}, []byte("testcabundle")),
				},
				hubClusterClient: clusterClient,
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, ""), "")
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestLabelDecorator(t *testing.T) {
	cases := []struct {
		name     string
		existing map[string]string
		input    map[string]string
		want     map[string]string
	}{
		{
			name:  "apply user labels on a cluster without labels",
			input: map[string]string{"env": "prod"},
			want:  map[string]string{"env": "prod"},
		},
		{
			name:  "reserved labels are dropped",
			input: map[string]string{"env": "prod", clusterv1beta2.ClusterSetLabel: "team-a"},
			want:  map[string]string{"env": "prod"},
		},
		{
			name:     "existing labels are not overwritten",
			existing: map[string]string{"env": "staging"},
			input:    map[string]string{"env": "prod", "tier": "gold"},
			want:     map[string]string{"env": "staging", "tier": "gold"},
		},
		{
			name:     "existing reserved label is preserved and injected reserved value is ignored",
			existing: map[string]string{clusterv1beta2.ClusterSetLabel: "keep-me"},
			input:    map[string]string{clusterv1beta2.ClusterSetLabel: "attacker", "env": "prod"},
			want:     map[string]string{clusterv1beta2.ClusterSetLabel: "keep-me", "env": "prod"},
		},
		{
			name:  "nil input is safe",
			input: nil,
			want:  map[string]string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cluster := &clusterv1.ManagedCluster{}
			if c.existing != nil {
				cluster.Labels = c.existing
			}
			got := LabelDecorator(c.input)(cluster)
			if !reflect.DeepEqual(got.Labels, c.want) {
				t.Errorf("LabelDecorator() labels = %v, want %v", got.Labels, c.want)
			}
		})
	}
}
