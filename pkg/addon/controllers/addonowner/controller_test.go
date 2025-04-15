package addonowner

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func newClusterManagementOwner(name string) metav1.OwnerReference {
	clusterManagementAddon := addontesting.NewClusterManagementAddon(name, "testcrd", "testcr").Build()
	return *metav1.NewControllerRef(clusterManagementAddon, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKey:                "test/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                   "no managedclusteraddon to sync",
			syncKey:                "cluster1/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "update managedclusteraddon",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test")),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			validateAddonActions:   addontesting.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append(c.clusterManagementAddon, c.managedClusteraddon...) //nolint:gocritic
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)
			recorder := syncContext.Recorder()

			controller := NewAddonOwnerController(
				fakeAddonClient,
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
				addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
				utils.ManagedByAddonManager,
				recorder)

			err := controller.Sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
