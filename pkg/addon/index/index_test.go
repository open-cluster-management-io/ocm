package index

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

func TestIndexClusterManagementAddonByPlacement(t *testing.T) {
	cases := []struct {
		name     string
		obj      interface{}
		expected []string
		wantErr  bool
	}{
		{
			name:     "invalid object type",
			obj:      "invalid",
			expected: []string{},
			wantErr:  true,
		},
		{
			name: "empty install strategy",
			obj: &addonv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Spec: addonv1alpha1.ClusterManagementAddOnSpec{},
			},
			expected: nil,
			wantErr:  false,
		},
		{
			name: "manual install strategy",
			obj: &addonv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Spec: addonv1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: addonv1alpha1.InstallStrategy{
						Type: addonv1alpha1.AddonInstallStrategyManual,
					},
				},
			},
			expected: nil,
			wantErr:  false,
		},
		{
			name: "placements install strategy with single placement",
			obj: &addonv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Spec: addonv1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: addonv1alpha1.InstallStrategy{
						Type: addonv1alpha1.AddonInstallStrategyPlacements,
						Placements: []addonv1alpha1.PlacementStrategy{
							{
								PlacementRef: addonv1alpha1.PlacementRef{
									Name:      "test-placement",
									Namespace: "test-namespace",
								},
							},
						},
					},
				},
			},
			expected: []string{"test-namespace/test-placement"},
			wantErr:  false,
		},
		{
			name: "placements install strategy with multiple placements",
			obj: &addonv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Spec: addonv1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: addonv1alpha1.InstallStrategy{
						Type: addonv1alpha1.AddonInstallStrategyPlacements,
						Placements: []addonv1alpha1.PlacementStrategy{
							{
								PlacementRef: addonv1alpha1.PlacementRef{
									Name:      "test-placement-1",
									Namespace: "test-namespace-1",
								},
							},
							{
								PlacementRef: addonv1alpha1.PlacementRef{
									Name:      "test-placement-2",
									Namespace: "test-namespace-2",
								},
							},
						},
					},
				},
			},
			expected: []string{"test-namespace-1/test-placement-1", "test-namespace-2/test-placement-2"},
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys, err := IndexClusterManagementAddonByPlacement(tc.obj)

			if tc.wantErr && err == nil {
				t.Errorf("expected error but got none")
				return
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(keys, tc.expected) {
				t.Errorf("expected keys %v, got %v", tc.expected, keys)
			}
		})
	}
}

func TestIndexManagedClusterAddonByName(t *testing.T) {
	cases := []struct {
		name     string
		obj      interface{}
		expected []string
		wantErr  bool
	}{
		{
			name:     "invalid object type",
			obj:      "invalid",
			expected: []string{},
			wantErr:  true,
		},
		{
			name: "valid ManagedClusterAddon",
			obj: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
			},
			expected: []string{"test-addon"},
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys, err := IndexManagedClusterAddonByName(tc.obj)

			if tc.wantErr && err == nil {
				t.Errorf("expected error but got none")
				return
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(keys, tc.expected) {
				t.Errorf("expected keys %v, got %v", tc.expected, keys)
			}
		})
	}
}

func TestClusterManagementAddonByPlacementQueueKey(t *testing.T) {
	cma1 := &addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-addon-1",
		},
		Spec: addonv1alpha1.ClusterManagementAddOnSpec{
			InstallStrategy: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyPlacements,
				Placements: []addonv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonv1alpha1.PlacementRef{
							Name:      "test-placement",
							Namespace: "test-namespace",
						},
					},
				},
			},
		},
	}

	cma2 := &addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-addon-2",
		},
		Spec: addonv1alpha1.ClusterManagementAddOnSpec{
			InstallStrategy: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyPlacements,
				Placements: []addonv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonv1alpha1.PlacementRef{
							Name:      "test-placement",
							Namespace: "test-namespace",
						},
					},
				},
			},
		},
	}

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-placement",
			Namespace: "test-namespace",
		},
	}

	fakeClient := fakeaddon.NewSimpleClientset(cma1, cma2)
	informerFactory := addoninformers.NewSharedInformerFactory(fakeClient, 0)
	cmaInformer := informerFactory.Addon().V1alpha1().ClusterManagementAddOns()

	cmaInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		ClusterManagementAddonByPlacement: IndexClusterManagementAddonByPlacement,
	})

	cmaInformer.Informer().GetStore().Add(cma1)
	cmaInformer.Informer().GetStore().Add(cma2)

	queueKeyFunc := ClusterManagementAddonByPlacementQueueKey(cmaInformer)

	cases := []struct {
		name     string
		obj      runtime.Object
		expected []string
	}{
		{
			name:     "placement object",
			obj:      placement,
			expected: []string{"test-addon-1", "test-addon-2"},
		},
		{
			name: "unrelated object",
			obj: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-placement",
					Namespace: "test-namespace",
				},
			},
			expected: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := queueKeyFunc(tc.obj)

			if len(keys) != len(tc.expected) {
				t.Errorf("expected %d keys, got %d", len(tc.expected), len(keys))
			}

			for _, expectedKey := range tc.expected {
				found := false
				for _, key := range keys {
					if key == expectedKey {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected key %s not found in result %v", expectedKey, keys)
				}
			}
		})
	}
}

func TestClusterManagementAddonByPlacementDecisionQueueKey(t *testing.T) {
	cma1 := &addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-addon-1",
		},
		Spec: addonv1alpha1.ClusterManagementAddOnSpec{
			InstallStrategy: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyPlacements,
				Placements: []addonv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonv1alpha1.PlacementRef{
							Name:      "test-placement",
							Namespace: "test-namespace",
						},
					},
				},
			},
		},
	}

	placementDecision := &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-placement-decision",
			Namespace: "test-namespace",
			Labels: map[string]string{
				clusterv1beta1.PlacementLabel: "test-placement",
			},
		},
	}

	fakeClient := fakeaddon.NewSimpleClientset(cma1)
	informerFactory := addoninformers.NewSharedInformerFactory(fakeClient, 0)
	cmaInformer := informerFactory.Addon().V1alpha1().ClusterManagementAddOns()

	cmaInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		ClusterManagementAddonByPlacement: IndexClusterManagementAddonByPlacement,
	})

	cmaInformer.Informer().GetStore().Add(cma1)

	queueKeyFunc := ClusterManagementAddonByPlacementDecisionQueueKey(cmaInformer)

	cases := []struct {
		name     string
		obj      runtime.Object
		expected []string
	}{
		{
			name:     "placement decision with placement label",
			obj:      placementDecision,
			expected: []string{"test-addon-1"},
		},
		{
			name: "placement decision without placement label",
			obj: &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-decision-no-label",
					Namespace: "test-namespace",
				},
			},
			expected: []string{},
		},
		{
			name: "placement decision with wrong placement label",
			obj: &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-decision-wrong",
					Namespace: "test-namespace",
					Labels: map[string]string{
						clusterv1beta1.PlacementLabel: "wrong-placement",
					},
				},
			},
			expected: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := queueKeyFunc(tc.obj)

			if !reflect.DeepEqual(keys, tc.expected) {
				t.Errorf("expected keys %v, got %v", tc.expected, keys)
			}
		})
	}
}

func TestManagedClusterAddonByNameQueueKey(t *testing.T) {
	mca1 := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-addon",
			Namespace: "cluster1",
		},
	}

	mca2 := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-addon",
			Namespace: "cluster2",
		},
	}

	testObj := &addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-addon",
		},
	}

	fakeClient := fakeaddon.NewSimpleClientset(mca1, mca2)
	informerFactory := addoninformers.NewSharedInformerFactory(fakeClient, 0)
	mcaInformer := informerFactory.Addon().V1alpha1().ManagedClusterAddOns()

	mcaInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		ManagedClusterAddonByName: IndexManagedClusterAddonByName,
	})

	mcaInformer.Informer().GetStore().Add(mca1)
	mcaInformer.Informer().GetStore().Add(mca2)

	queueKeyFunc := ManagedClusterAddonByNameQueueKey(mcaInformer)

	cases := []struct {
		name     string
		obj      runtime.Object
		expected []string
	}{
		{
			name:     "object with name matching ManagedClusterAddons",
			obj:      testObj,
			expected: []string{"cluster1/test-addon", "cluster2/test-addon"},
		},
		{
			name: "object with name not matching any ManagedClusterAddons",
			obj: &addonv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "non-existent-addon",
				},
			},
			expected: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := queueKeyFunc(tc.obj)

			if len(keys) != len(tc.expected) {
				t.Errorf("expected %d keys, got %d", len(tc.expected), len(keys))
			}

			for _, expectedKey := range tc.expected {
				found := false
				for _, key := range keys {
					if key == expectedKey {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected key %s not found in result %v", expectedKey, keys)
				}
			}
		})
	}
}
