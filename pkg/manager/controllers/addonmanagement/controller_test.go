package addonmanagement

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/index"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", ""),
			placements:             []runtime.Object{},
			placementDecisions:     []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                "manual installStrategy",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "")
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
			name:                "placement is missting",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "")
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManualPlacements,
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
			name:                "placement decision is missting",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: func() *addonv1alpha1.ClusterManagementAddOn {
				addon := addontesting.NewClusterManagementAddon("test", "", "")
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManualPlacements,
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
				addon := addontesting.NewClusterManagementAddon("test", "", "")
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManualPlacements,
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
				addon := addontesting.NewClusterManagementAddon("test", "", "")
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManualPlacements,
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
				addon := addontesting.NewClusterManagementAddon("test", "", "")
				addon.Spec.InstallStrategy = addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManualPlacements,
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
			clusterObj := append(c.placements, c.placementDecisions...)
			fakeClusterClient := fakecluster.NewSimpleClientset(clusterObj...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
				cache.Indexers{
					index.ManagedClusterAddonByName: index.IndexManagedClusterAddonByName,
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
