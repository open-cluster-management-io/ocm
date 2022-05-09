package addon

import (
	"context"
	"testing"
	"time"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

func TestSync(t *testing.T) {
	cases := []struct {
		name            string
		managedClusters []runtime.Object
		addOns          []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "managed cluster is deleted",
			managedClusters: []runtime.Object{},
			addOns:          []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:            "managed cluster is not accepted",
			managedClusters: []runtime.Object{testinghelpers.NewManagedCluster()},
			addOns:          []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:            "managed cluster is available",
			managedClusters: []runtime.Object{testinghelpers.NewAvailableManagedCluster()},
			addOns:          []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:            "managed cluster is unknown",
			managedClusters: []runtime.Object{testinghelpers.NewUnknownManagedCluster()},
			addOns: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Namespace: testinghelpers.TestManagedClusterName, Name: "test"},
			}},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "update")

				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonv1alpha1.ManagedClusterAddOn)
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, "Available")
				if addOnCond == nil {
					t.Errorf("expected addon available condition, but failed")
					return
				}
				if addOnCond.Status != metav1.ConditionUnknown {
					t.Errorf("expected addon available condition is unknown, but failed")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.managedClusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.managedClusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			addOnClient := addonfake.NewSimpleClientset(c.addOns...)
			addOnInformerFactory := addoninformers.NewSharedInformerFactory(addOnClient, time.Minute*10)
			addOnStroe := addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
			for _, addOn := range c.addOns {
				if err := addOnStroe.Add(addOn); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &managedClusterAddOnHealthCheckController{
				addOnClient:   addOnClient,
				addOnLister:   addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
			}

			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, addOnClient.Actions())
		})
	}
}
