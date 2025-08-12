package registration

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

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

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, ""))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestCreateSpokeClusterWithLabels(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		labels          map[string]string
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "create a new cluster with labels",
			startingObjects: []runtime.Object{},
			labels: map[string]string{
				"environment": "test",
				"agent.open-cluster-management.io/cluster-type": "spoke",
			},
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

				// Verify labels
				clusterLabels := actual.(*clusterv1.ManagedCluster).Labels
				if len(clusterLabels) != 2 {
					t.Errorf("expected cluster labels %#v but got: %#v", 2, len(clusterLabels))
				}
				if value, ok := clusterLabels["environment"]; !ok || value != "test" {
					t.Errorf("expected cluster label environment=test but got: %#v",
						clusterLabels["environment"])
				}
				if value, ok := clusterLabels["agent.open-cluster-management.io/cluster-type"]; !ok || value != "spoke" {
					t.Errorf("expected cluster label agent.open-cluster-management.io/cluster-type=spoke but got: %#v",
						clusterLabels["agent.open-cluster-management.io/cluster-type"])
				}
			},
		},
		{
			name:            "create cluster with empty labels",
			startingObjects: []runtime.Object{},
			labels:          map[string]string{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "create")
				actual := actions[1].(clienttesting.CreateActionImpl).Object

				// Verify no labels
				clusterLabels := actual.(*clusterv1.ManagedCluster).Labels
				if len(clusterLabels) != 0 {
					t.Errorf("expected no cluster labels but got: %#v", clusterLabels)
				}
			},
		},
		{
			name:            "update an existing cluster with labels",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			labels: map[string]string{
				"region": "us-west-2",
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object

				// Verify labels
				clusterLabels := actual.(*clusterv1.ManagedCluster).Labels
				if len(clusterLabels) != 1 {
					t.Errorf("expected cluster labels %#v but got: %#v", 1, len(clusterLabels))
				}
				if value, ok := clusterLabels["region"]; !ok || value != "us-west-2" {
					t.Errorf("expected cluster label region=us-west-2 but got: %#v",
						clusterLabels["region"])
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			ctrl := managedClusterCreatingController{
				clusterName: testinghelpers.TestManagedClusterName,
				clusterDecorators: []ManagedClusterDecorator{
					LabelDecorator(c.labels),
					ClientConfigDecorator([]string{testSpokeExternalServerUrl}, []byte("testcabundle")),
				},
				hubClusterClient: clusterClient,
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, ""))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestCreateSpokeClusterWithLabelsAndAnnotations(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "create cluster with both labels and annotations",
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

				// Verify labels
				clusterLabels := actual.(*clusterv1.ManagedCluster).Labels
				if len(clusterLabels) != 2 {
					t.Errorf("expected cluster labels %#v but got: %#v", 2, len(clusterLabels))
				}
				if value, ok := clusterLabels["env"]; !ok || value != "production" {
					t.Errorf("expected cluster label env=production but got: %#v",
						clusterLabels["env"])
				}
				if value, ok := clusterLabels["cluster-role"]; !ok || value != "worker" {
					t.Errorf("expected cluster label cluster-role=worker but got: %#v",
						clusterLabels["cluster-role"])
				}

				// Verify annotations
				clusterAnnotations := actual.(*clusterv1.ManagedCluster).Annotations
				if len(clusterAnnotations) != 1 {
					t.Errorf("expected cluster annotations %#v but got: %#v", 1, len(clusterAnnotations))
				}
				if value, ok := clusterAnnotations["agent.open-cluster-management.io/test"]; !ok || value != "true" {
					t.Errorf("expected cluster annotation agent.open-cluster-management.io/test=true but got: %#v",
						clusterAnnotations["agent.open-cluster-management.io/test"])
				}
			},
		},
		{
			name:            "update existing cluster preserves both labels and annotations",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object

				// Verify labels are updated
				clusterLabels := actual.(*clusterv1.ManagedCluster).Labels
				if len(clusterLabels) != 2 {
					t.Errorf("expected cluster labels %#v but got: %#v", 2, len(clusterLabels))
				}

				// Verify annotations are updated
				clusterAnnotations := actual.(*clusterv1.ManagedCluster).Annotations
				if len(clusterAnnotations) != 1 {
					t.Errorf("expected cluster annotations %#v but got: %#v", 1, len(clusterAnnotations))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			ctrl := managedClusterCreatingController{
				clusterName: testinghelpers.TestManagedClusterName,
				clusterDecorators: []ManagedClusterDecorator{
					LabelDecorator(map[string]string{
						"env":          "production",
						"cluster-role": "worker",
					}),
					AnnotationDecorator(map[string]string{
						"agent.open-cluster-management.io/test": "true",
					}),
					ClientConfigDecorator([]string{testSpokeExternalServerUrl}, []byte("testcabundle")),
				},
				hubClusterClient: clusterClient,
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, ""))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestEqualityHelpers(t *testing.T) {
	t.Run("equalLabels", func(t *testing.T) {
		tests := []struct {
			name     string
			labels1  map[string]string
			labels2  map[string]string
			expected bool
		}{
			{
				name:     "equal empty maps",
				labels1:  map[string]string{},
				labels2:  map[string]string{},
				expected: true,
			},
			{
				name:     "equal nil and empty map",
				labels1:  nil,
				labels2:  map[string]string{},
				expected: true,
			},
			{
				name: "equal non-empty maps",
				labels1: map[string]string{
					"env":    "test",
					"region": "us-west",
				},
				labels2: map[string]string{
					"env":    "test",
					"region": "us-west",
				},
				expected: true,
			},
			{
				name: "different values",
				labels1: map[string]string{
					"env": "test",
				},
				labels2: map[string]string{
					"env": "prod",
				},
				expected: false,
			},
			{
				name: "different keys",
				labels1: map[string]string{
					"env": "test",
				},
				labels2: map[string]string{
					"environment": "test",
				},
				expected: false,
			},
			{
				name: "different lengths",
				labels1: map[string]string{
					"env": "test",
				},
				labels2: map[string]string{
					"env":    "test",
					"region": "us-west",
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := equalLabels(tt.labels1, tt.labels2)
				if result != tt.expected {
					t.Errorf("equalLabels(%v, %v) = %v, expected %v",
						tt.labels1, tt.labels2, result, tt.expected)
				}
			})
		}
	})

	t.Run("equalAnnotations", func(t *testing.T) {
		tests := []struct {
			name         string
			annotations1 map[string]string
			annotations2 map[string]string
			expected     bool
		}{
			{
				name:         "equal empty maps",
				annotations1: map[string]string{},
				annotations2: map[string]string{},
				expected:     true,
			},
			{
				name:         "equal nil and empty map",
				annotations1: nil,
				annotations2: map[string]string{},
				expected:     true,
			},
			{
				name: "equal non-empty maps",
				annotations1: map[string]string{
					"agent.open-cluster-management.io/test": "true",
					"custom.annotation":                     "value",
				},
				annotations2: map[string]string{
					"agent.open-cluster-management.io/test": "true",
					"custom.annotation":                     "value",
				},
				expected: true,
			},
			{
				name: "different values",
				annotations1: map[string]string{
					"agent.open-cluster-management.io/test": "true",
				},
				annotations2: map[string]string{
					"agent.open-cluster-management.io/test": "false",
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := equalAnnotations(tt.annotations1, tt.annotations2)
				if result != tt.expected {
					t.Errorf("equalAnnotations(%v, %v) = %v, expected %v",
						tt.annotations1, tt.annotations2, result, tt.expected)
				}
			})
		}
	})

	t.Run("equalClientConfigs", func(t *testing.T) {
		tests := []struct {
			name     string
			configs1 []clusterv1.ClientConfig
			configs2 []clusterv1.ClientConfig
			expected bool
		}{
			{
				name:     "equal empty slices",
				configs1: []clusterv1.ClientConfig{},
				configs2: []clusterv1.ClientConfig{},
				expected: true,
			},
			{
				name:     "equal nil and empty slice",
				configs1: nil,
				configs2: []clusterv1.ClientConfig{},
				expected: true,
			},
			{
				name: "equal non-empty slices",
				configs1: []clusterv1.ClientConfig{
					{
						URL:      "https://test.example.com",
						CABundle: []byte("test-ca-bundle"),
					},
				},
				configs2: []clusterv1.ClientConfig{
					{
						URL:      "https://test.example.com",
						CABundle: []byte("test-ca-bundle"),
					},
				},
				expected: true,
			},
			{
				name: "different lengths",
				configs1: []clusterv1.ClientConfig{
					{URL: "https://test1.example.com"},
				},
				configs2: []clusterv1.ClientConfig{
					{URL: "https://test1.example.com"},
					{URL: "https://test2.example.com"},
				},
				expected: false,
			},
			{
				name: "different URLs",
				configs1: []clusterv1.ClientConfig{
					{URL: "https://test1.example.com"},
				},
				configs2: []clusterv1.ClientConfig{
					{URL: "https://test2.example.com"},
				},
				expected: false,
			},
			{
				name: "different CA bundles",
				configs1: []clusterv1.ClientConfig{
					{
						URL:      "https://test.example.com",
						CABundle: []byte("bundle1"),
					},
				},
				configs2: []clusterv1.ClientConfig{
					{
						URL:      "https://test.example.com",
						CABundle: []byte("bundle2"),
					},
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := equalClientConfigs(tt.configs1, tt.configs2)
				if result != tt.expected {
					t.Errorf("equalClientConfigs(%v, %v) = %v, expected %v",
						tt.configs1, tt.configs2, result, tt.expected)
				}
			})
		}
	})
}
