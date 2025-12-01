package addonmanagement

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2/ktesting"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestAddonInstallReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon *addonv1alpha1.ClusterManagementAddOn
		placements             []runtime.Object
		placementDecisions     []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
		expectErr              bool
	}{
		{
			name:                   "no installStrategy",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").Build(),
			placements:             []runtime.Object{},
			placementDecisions:     []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                "manual installStrategy",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "").Build()
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManual,
				}
				return addon
			}(),
			placements:           []runtime.Object{},
			placementDecisions:   []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                "placement is missing",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "").Build()
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
						},
					},
				}
				return addon
			}(),
			placements:           []runtime.Object{},
			placementDecisions:   []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                "placement decision is missing",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "").Build()
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
						},
					},
				}
				return addon
			}(),
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions:   []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                "install addon",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "").Build()
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
						},
					},
				}
				return addon
			}(),
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create", "create")
			},
		},
		{
			name: "addon/remove addon",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster0"),
				addontesting.NewAddon("test", "cluster1"),
			},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "").Build()
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
						},
					},
				}
				return addon
			}(),
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create", "delete")
			},
		},
		{
			name: "multiple placements",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster0"),
				addontesting.NewAddon("test", "cluster1"),
			},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "").Build()
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
						},
						{
							PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement1", Namespace: "default"},
						},
					},
				}
				return addon
			}(),
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement1", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement1",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement1"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create", "create", "delete")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterObj := append(c.placements, c.placementDecisions...) //nolint:gocritic
			fakeClusterClient := fakecluster.NewSimpleClientset(clusterObj...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
				cache.Indexers{
					addonindex.ManagedClusterAddonByName: addonindex.IndexManagedClusterAddonByName,
				})
			if err != nil {
				t.Fatal(err)
			}

			for _, obj := range c.placements {
				if err := clusterInformers.Cluster().V1beta1().Placements().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, obj := range c.placementDecisions {
				if err := clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			reconcile := &managedClusterAddonInstallReconciler{
				addonClient:                fakeAddonClient,
				placementLister:            clusterInformers.Cluster().V1beta1().Placements().Lister(),
				placementDecisionLister:    clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
				managedClusterAddonIndexer: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetIndexer(),
				addonFilterFunc:            utils.ManagedByAddonManager,
			}

			_, _, err = reconcile.reconcile(context.TODO(), c.clusterManagementAddon)
			if err != nil && !c.expectErr {
				t.Errorf("expected no error when sync: %v", err)
			}
			if err == nil && c.expectErr {
				t.Errorf("Expect error but got no error")
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}

func TestNewAddonManagementController(t *testing.T) {
	fakeAddonClient := fakeaddon.NewSimpleClientset()
	fakeClusterClient := fakecluster.NewSimpleClientset()

	addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
	clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

	// Add required indexer
	err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
		cache.Indexers{
			addonindex.ManagedClusterAddonByName: addonindex.IndexManagedClusterAddonByName,
		})
	if err != nil {
		t.Fatal(err)
	}

	addonFilterFunc := func(obj interface{}) bool {
		return true
	}

	controller := NewAddonManagementController(
		fakeAddonClient,
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
		addonFilterFunc,
	)

	if controller == nil {
		t.Error("Expected controller to be created, got nil")
	}

	// Verify controller is of correct type
	if _, ok := controller.(factory.Controller); !ok {
		t.Error("Expected controller to implement factory.Controller interface")
	}
}

func TestAddonManagementControllerSync(t *testing.T) {
	cases := []struct {
		name                    string
		queueKey                string
		managedClusterAddons    []runtime.Object
		clusterManagementAddons []runtime.Object
		placements              []runtime.Object
		placementDecisions      []runtime.Object
		expectError             bool
		expectedErrorMessage    string
	}{
		{
			name:     "addon not found",
			queueKey: "non-existent-addon",
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddons: []runtime.Object{},
			expectError:             false,
		},
		{
			name:     "basic addon sync",
			queueKey: "test-addon",
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddons: []runtime.Object{
				addontesting.NewClusterManagementAddon("test-addon", "", "").Build(),
			},
			placements:         []runtime.Object{},
			placementDecisions: []runtime.Object{},
			expectError:        false,
		},
		{
			name:     "addon with placement strategy",
			queueKey: "test-addon",
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddons: []runtime.Object{
				func() *addonv1alpha1.ClusterManagementAddOn {
					addon := addontesting.NewClusterManagementAddon("test-addon", "", "").Build()
					addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
						Type: addonv1alpha1.AddonInstallStrategyPlacements,
						Placements: []addonv1alpha1.PlacementStrategy{
							{
								PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
							},
						},
					}
					return addon
				}(),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
					},
				},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel: "test-placement",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{
							{ClusterName: "cluster1"},
							{ClusterName: "cluster2"},
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _ = ktesting.NewTestContext(t)

			fakeAddonClient := fakeaddon.NewSimpleClientset(append(c.managedClusterAddons, c.clusterManagementAddons...)...)
			clusterObjs := append(c.placements, c.placementDecisions...)
			fakeClusterClient := fakecluster.NewSimpleClientset(clusterObjs...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			// Add required indexer
			err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
				cache.Indexers{
					addonindex.ManagedClusterAddonByName: addonindex.IndexManagedClusterAddonByName,
				})
			if err != nil {
				t.Fatal(err)
			}

			// Populate informer stores
			for _, obj := range c.managedClusterAddons {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, obj := range c.clusterManagementAddons {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, obj := range c.placements {
				if err := clusterInformers.Cluster().V1beta1().Placements().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, obj := range c.placementDecisions {
				if err := clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			// Create controller
			addonFilterFunc := func(obj interface{}) bool {
				return true
			}

			controller := &addonManagementController{
				addonClient:                   fakeAddonClient,
				clusterManagementAddonLister:  addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				clusterManagementAddonIndexer: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetIndexer(),
				reconcilers: []addonManagementReconcile{
					&managedClusterAddonInstallReconciler{
						addonClient:                fakeAddonClient,
						placementDecisionLister:    clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
						placementLister:            clusterInformers.Cluster().V1beta1().Placements().Lister(),
						managedClusterAddonIndexer: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetIndexer(),
						addonFilterFunc:            addonFilterFunc,
					},
				},
			}

			// Create sync context
			syncCtx := testingcommon.NewFakeSyncContext(t, c.queueKey)

			// Test sync method
			ctx := context.TODO()
			err = controller.sync(ctx, syncCtx, c.queueKey)

			if c.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !c.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if c.expectedErrorMessage != "" && (err == nil || err.Error() != c.expectedErrorMessage) {
				t.Errorf("Expected error message '%s', got '%v'", c.expectedErrorMessage, err)
			}
		})
	}
}
