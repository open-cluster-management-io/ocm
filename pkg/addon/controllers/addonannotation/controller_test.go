package addonannotation

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestAddonAnnotationsChanged(t *testing.T) {
	cases := []struct {
		name           string
		oldAnnotations map[string]string
		newAnnotations map[string]string
		expected       bool
	}{
		{
			name:           "both nil",
			oldAnnotations: nil,
			newAnnotations: nil,
			expected:       false,
		},
		{
			name:           "both empty",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{},
			expected:       false,
		},
		{
			name:           "no addon annotations, other annotations changed",
			oldAnnotations: map[string]string{"foo": "bar"},
			newAnnotations: map[string]string{"foo": "baz"},
			expected:       false,
		},
		{
			name:           "addon annotation added",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{"addon.open-cluster-management.io/key": "val"},
			expected:       true,
		},
		{
			name:           "addon annotation removed",
			oldAnnotations: map[string]string{"addon.open-cluster-management.io/key": "val"},
			newAnnotations: map[string]string{},
			expected:       true,
		},
		{
			name:           "addon annotation value changed",
			oldAnnotations: map[string]string{"addon.open-cluster-management.io/key": "val1"},
			newAnnotations: map[string]string{"addon.open-cluster-management.io/key": "val2"},
			expected:       true,
		},
		{
			name:           "addon annotations unchanged, other annotations changed",
			oldAnnotations: map[string]string{"addon.open-cluster-management.io/key": "val", "foo": "bar"},
			newAnnotations: map[string]string{"addon.open-cluster-management.io/key": "val", "foo": "baz"},
			expected:       false,
		},
		{
			name: "multiple addon annotations unchanged",
			oldAnnotations: map[string]string{
				"addon.open-cluster-management.io/key1": "val1",
				"addon.open-cluster-management.io/key2": "val2",
			},
			newAnnotations: map[string]string{
				"addon.open-cluster-management.io/key1": "val1",
				"addon.open-cluster-management.io/key2": "val2",
			},
			expected: false,
		},
		{
			name: "one of multiple addon annotations changed",
			oldAnnotations: map[string]string{
				"addon.open-cluster-management.io/key1": "val1",
				"addon.open-cluster-management.io/key2": "val2",
			},
			newAnnotations: map[string]string{
				"addon.open-cluster-management.io/key1": "val1",
				"addon.open-cluster-management.io/key2": "changed",
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := addonAnnotationsChanged(c.oldAnnotations, c.newAnnotations)
			if result != c.expected {
				t.Errorf("expected %v, got %v", c.expected, result)
			}
		})
	}
}

func TestSyncAddonAnnotations(t *testing.T) {
	cases := []struct {
		name                    string
		addonAnnotations        map[string]string
		clusterAddonAnnotations map[string]string
		expectChanged           bool
		expectedAnnotations     map[string]string
	}{
		{
			name:                    "no change needed, both empty",
			addonAnnotations:        nil,
			clusterAddonAnnotations: map[string]string{},
			expectChanged:           false,
		},
		{
			name:             "add addon annotations to addon",
			addonAnnotations: nil,
			clusterAddonAnnotations: map[string]string{
				"addon.open-cluster-management.io/hosting-cluster-name": "hosting",
			},
			expectChanged: true,
			expectedAnnotations: map[string]string{
				"addon.open-cluster-management.io/hosting-cluster-name": "hosting",
			},
		},
		{
			name: "remove stale addon annotation from addon",
			addonAnnotations: map[string]string{
				"addon.open-cluster-management.io/old-key": "old-val",
			},
			clusterAddonAnnotations: map[string]string{},
			expectChanged:           true,
			expectedAnnotations:     map[string]string{},
		},
		{
			name: "update addon annotation value",
			addonAnnotations: map[string]string{
				"addon.open-cluster-management.io/key": "old-val",
			},
			clusterAddonAnnotations: map[string]string{
				"addon.open-cluster-management.io/key": "new-val",
			},
			expectChanged: true,
			expectedAnnotations: map[string]string{
				"addon.open-cluster-management.io/key": "new-val",
			},
		},
		{
			name: "non-addon annotations preserved",
			addonAnnotations: map[string]string{
				"my-custom-annotation": "should-stay",
			},
			clusterAddonAnnotations: map[string]string{
				"addon.open-cluster-management.io/key": "val",
			},
			expectChanged: true,
			expectedAnnotations: map[string]string{
				"my-custom-annotation":                 "should-stay",
				"addon.open-cluster-management.io/key": "val",
			},
		},
		{
			name: "no change when already in sync",
			addonAnnotations: map[string]string{
				"addon.open-cluster-management.io/key": "val",
				"my-custom-annotation":                 "custom",
			},
			clusterAddonAnnotations: map[string]string{
				"addon.open-cluster-management.io/key": "val",
			},
			expectChanged: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addon := &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-addon",
					Namespace:   "cluster1",
					Annotations: c.addonAnnotations,
				},
			}

			changed, updatedAddon := syncAddonAnnotations(addon, c.clusterAddonAnnotations)
			if changed != c.expectChanged {
				t.Errorf("expected changed=%v, got %v", c.expectChanged, changed)
			}

			if c.expectedAnnotations != nil {
				if len(updatedAddon.Annotations) != len(c.expectedAnnotations) {
					t.Errorf("expected %d annotations, got %d: %v",
						len(c.expectedAnnotations), len(updatedAddon.Annotations), updatedAddon.Annotations)
				}
				for k, v := range c.expectedAnnotations {
					if updatedAddon.Annotations[k] != v {
						t.Errorf("expected annotation %s=%s, got %s", k, v, updatedAddon.Annotations[k])
					}
				}
			}
		})
	}
}

func TestSync(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusters        []runtime.Object
		managedClusterAddons   []runtime.Object
		clusterManagementAddon []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
		expectErr              bool
	}{
		{
			name:                   "cluster not found",
			syncKey:                "non-existent-cluster",
			managedClusters:        []runtime.Object{},
			managedClusterAddons:   []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "cluster with no addon annotations",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "cluster1",
						Annotations: map[string]string{"other-annotation": "value"},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{
				newCMAWithPlacementStrategy("test-addon"),
			},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "no addons in cluster namespace",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/key": "val",
						},
					},
				},
			},
			managedClusterAddons:   []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "skip addon with no clustermanagementaddon",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/key": "val",
						},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "skip addon with manual install strategy",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/key": "val",
						},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{
				func() *addonv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("test-addon", "", "").Build()
					cma.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
						Type: addonv1alpha1.AddonInstallStrategyManual,
					}
					return cma
				}(),
			},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "skip addon with empty install strategy type",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/key": "val",
						},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test-addon", "", "").Build(),
			},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "sync addon annotations from cluster to addon",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/hosting-cluster-name": "hosting-cluster",
							"addon.open-cluster-management.io/on-multicluster-hub":  "true",
							"non-addon-annotation":                                  "should-not-be-synced",
						},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{
				newCMAWithPlacementStrategy("test-addon"),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).GetObject()
				addon := actual.(*addonv1alpha1.ManagedClusterAddOn)
				if len(addon.Annotations) != 2 {
					t.Errorf("expected 2 annotations, got %d: %v", len(addon.Annotations), addon.Annotations)
				}
				if addon.Annotations["addon.open-cluster-management.io/hosting-cluster-name"] != "hosting-cluster" {
					t.Errorf("expected hosting-cluster-name annotation, got %v", addon.Annotations)
				}
				if addon.Annotations["addon.open-cluster-management.io/on-multicluster-hub"] != "true" {
					t.Errorf("expected on-multicluster-hub annotation, got %v", addon.Annotations)
				}
				if _, ok := addon.Annotations["non-addon-annotation"]; ok {
					t.Errorf("non-addon annotation should not be synced")
				}
			},
		},
		{
			name:    "no update when addon already has correct annotations",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/key": "val",
						},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				func() *addonv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test-addon", "cluster1")
					addon.Annotations = map[string]string{
						"addon.open-cluster-management.io/key": "val",
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{
				newCMAWithPlacementStrategy("test-addon"),
			},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "remove stale addon annotations from addon",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				func() *addonv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test-addon", "cluster1")
					addon.Annotations = map[string]string{
						"addon.open-cluster-management.io/old-key": "old-val",
						"my-custom-annotation":                     "should-stay",
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{
				newCMAWithPlacementStrategy("test-addon"),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).GetObject()
				addon := actual.(*addonv1alpha1.ManagedClusterAddOn)
				if len(addon.Annotations) != 1 {
					t.Errorf("expected 1 annotation, got %d: %v", len(addon.Annotations), addon.Annotations)
				}
				if addon.Annotations["my-custom-annotation"] != "should-stay" {
					t.Errorf("custom annotation should be preserved, got %v", addon.Annotations)
				}
				if _, ok := addon.Annotations["addon.open-cluster-management.io/old-key"]; ok {
					t.Errorf("stale addon annotation should be removed")
				}
			},
		},
		{
			name:    "sync annotations to multiple addons",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/key": "val",
						},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("addon1", "cluster1"),
				addontesting.NewAddon("addon2", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{
				newCMAWithPlacementStrategy("addon1"),
				newCMAWithPlacementStrategy("addon2"),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update", "update")
			},
		},
		{
			name:    "mixed: sync one addon, skip manual addon",
			syncKey: "cluster1",
			managedClusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/key": "val",
						},
					},
				},
			},
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("placement-addon", "cluster1"),
				addontesting.NewAddon("manual-addon", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{
				newCMAWithPlacementStrategy("placement-addon"),
				func() *addonv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("manual-addon", "", "").Build()
					cma.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
						Type: addonv1alpha1.AddonInstallStrategyManual,
					}
					return cma
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).GetObject()
				addon := actual.(*addonv1alpha1.ManagedClusterAddOn)
				if addon.Name != "placement-addon" {
					t.Errorf("expected placement-addon to be updated, got %s", addon.Name)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(
				append(c.managedClusterAddons, c.clusterManagementAddon...)...)
			fakeClusterClient := fakecluster.NewSimpleClientset(c.managedClusters...)

			addonInformerFactory := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformerFactory := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			for _, obj := range c.managedClusterAddons {
				if err := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.managedClusters {
				if err := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := &addonAnnotationController{
				addonClient:                  fakeAddonClient,
				managedClusterLister:         clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister:    addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				clusterManagementAddonLister: addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)
			err := controller.sync(context.TODO(), syncContext, c.syncKey)
			if err != nil && !c.expectErr {
				t.Errorf("expected no error when sync: %v", err)
			}
			if err == nil && c.expectErr {
				t.Errorf("expected error but got none")
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}

func newCMAWithPlacementStrategy(name string) *addonv1alpha1.ClusterManagementAddOn {
	cma := addontesting.NewClusterManagementAddon(name, "", "").Build()
	cma.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
		Type: addonv1alpha1.AddonInstallStrategyPlacements,
		Placements: []addonv1alpha1.PlacementStrategy{
			{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
			},
		},
	}
	return cma
}
