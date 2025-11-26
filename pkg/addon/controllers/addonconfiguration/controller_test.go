package addonconfiguration

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2/ktesting"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	"open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestNewAddonConfigurationController(t *testing.T) {
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

	controller := NewAddonConfigurationController(
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

func TestAddonConfigurationControllerSync(t *testing.T) {
	cases := []struct {
		name                    string
		queueKey                string
		managedClusterAddons    []runtime.Object
		clusterManagementAddons []runtime.Object
		placements              []runtime.Object
		placementDecisions      []runtime.Object
		expectError             bool
		expectedErrorMessage    string
		addonFilterFuncResult   bool
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
			name:     "addon filtered out",
			queueKey: "test-addon",
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
			},
			clusterManagementAddons: []runtime.Object{
				addontesting.NewClusterManagementAddon("test-addon", "", "").Build(),
			},
			addonFilterFuncResult: false,
			expectError:           false,
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
			placements:            []runtime.Object{},
			placementDecisions:    []runtime.Object{},
			addonFilterFuncResult: true,
			expectError:           false,
		},
		{
			name:     "addon with placement strategy",
			queueKey: "test-addon",
			managedClusterAddons: []runtime.Object{
				addontesting.NewAddon("test-addon", "cluster1"),
				addontesting.NewAddon("test-addon", "cluster2"),
			},
			clusterManagementAddons: []runtime.Object{
				addontesting.NewClusterManagementAddon("test-addon", "", "").
					WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
						PlacementRef: addonv1alpha1.PlacementRef{
							Name:      "test-placement",
							Namespace: "default",
						},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					}).
					WithInstallProgression(addonv1alpha1.InstallProgression{
						PlacementRef: addonv1alpha1.PlacementRef{
							Name:      "test-placement",
							Namespace: "default",
						},
					}).Build(),
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
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
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
			addonFilterFuncResult: true,
			expectError:           false,
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

			// Create controller with addon filter function
			addonFilterFunc := func(obj interface{}) bool {
				return c.addonFilterFuncResult
			}

			controller := &addonConfigurationController{
				addonClient:                  fakeAddonClient,
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonIndexer:   addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetIndexer(),
				placementLister:              clusterInformers.Cluster().V1beta1().Placements().Lister(),
				placementDecisionGetter: helpers.PlacementDecisionGetter{
					Client: clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
				},
				addonFilterFunc: addonFilterFunc,
				reconcilers: []addonConfigurationReconcile{
					&managedClusterAddonConfigurationReconciler{
						addonClient: fakeAddonClient,
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
