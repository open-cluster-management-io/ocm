package addonstatus

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
)

func newClusterManagementOwner(name string) metav1.OwnerReference {
	clusterManagementAddon := addontesting.NewClusterManagementAddon(name, "testcrd", "testcr")
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
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "update managedclusteraddon",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test")),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if len(addOn.Status.RelatedObjects) != 1 {
					t.Errorf("Related object is not correct: %v", addOn.Status.RelatedObjects)
				}
				if meta.IsStatusConditionTrue(addOn.Status.Conditions, UnsupportedConfigurationType) {
					t.Errorf("supportd configuration is not correct")
				}
			},
		},
		{
			name:    "no need to update managedclusteraddon",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					addon.Status.RelatedObjects = []addonapiv1alpha1.ObjectReference{
						{
							Name:     "test",
							Group:    "addon.open-cluster-management.io",
							Resource: "clustermanagementaddons",
						},
					}
					addon.Status.Conditions = []metav1.Condition{
						{
							Type:    UnsupportedConfigurationType,
							Status:  metav1.ConditionFalse,
							Reason:  "ConfigurationSupported",
							Message: "the config resources are supported",
						},
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "")},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "update related objects",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					addon.Status.RelatedObjects = []addonapiv1alpha1.ObjectReference{
						{
							Name:     "other",
							Group:    "otherGroupd",
							Resource: "otherresources",
						},
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if len(addOn.Status.RelatedObjects) != 2 {
					t.Errorf("Related object is not updated, %v", addOn.Status.RelatedObjects)
				}
			},
		},
		{
			name:    "unsupported configs in ManagedClusterAddOn",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1", newClusterManagementOwner("test"))
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "config1.test",
								Resource: "config1",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "override",
							},
						},
					}
					addon.Status.SupportedConfigs = []addonapiv1alpha1.ConfigGroupResource{
						{
							Group:    "configs.test",
							Resource: "testconfigs",
						},
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}

				if !meta.IsStatusConditionTrue(addOn.Status.Conditions, UnsupportedConfigurationType) {
					t.Errorf("Expect addon config is wrong, but failed")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append(c.clusterManagementAddon, c.managedClusteraddon...)
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

			controller := addonStatusController{
				addonClient:                  fakeAddonClient,
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
			}

			syncContext := addontesting.NewFakeSyncContext(t)
			err := controller.sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
