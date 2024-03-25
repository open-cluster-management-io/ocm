package cmamanagedby

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func newClusterManagementAddonWithAnnotation(name string, annotations map[string]string) *addonv1alpha1.ClusterManagementAddOn {
	cma := addontesting.NewClusterManagementAddon(name, "", "").Build()
	cma.Annotations = annotations
	return cma
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		syncKey              string
		cma                  []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:    "add annotation if no annotation",
			syncKey: "test",
			cma:     []runtime.Object{newClusterManagementAddonWithAnnotation("test", nil)},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Annotations) != 1 || cma.Annotations[addonv1alpha1.AddonLifecycleAnnotationKey] != addonv1alpha1.AddonLifecycleAddonManagerAnnotationValue {
					t.Errorf("cma annotation is not correct, expected addon-manager but got %s", cma.Annotations[addonv1alpha1.AddonLifecycleAnnotationKey])
				}
			},
		},
		{
			name:    "add annotation if addon.open-cluster-management.io/lifecycle is empty",
			syncKey: "test",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonv1alpha1.AddonLifecycleAnnotationKey: "",
			})},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Annotations) != 1 || cma.Annotations[addonv1alpha1.AddonLifecycleAnnotationKey] != addonv1alpha1.AddonLifecycleAddonManagerAnnotationValue {
					t.Errorf("cma annotation is not correct, expected addon-manager but got %s", cma.Annotations[addonv1alpha1.AddonLifecycleAnnotationKey])
				}
			},
		},
		{
			name:    "no patch annotation if managed by self",
			syncKey: "test",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonv1alpha1.AddonLifecycleAnnotationKey: addonv1alpha1.AddonLifecycleSelfManageAnnotationValue,
			})},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:    "no patch annotation if managed by addon-manager",
			syncKey: "test",
			cma: []runtime.Object{newClusterManagementAddonWithAnnotation("test", map[string]string{
				"test": "test",
				addonv1alpha1.AddonLifecycleAnnotationKey: addonv1alpha1.AddonLifecycleAddonManagerAnnotationValue,
			})},
			validateAddonActions: addontesting.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.cma...)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

			for _, obj := range c.cma {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)
			recorder := syncContext.Recorder()

			controller := NewCMAManagedByController(
				fakeAddonClient,
				addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
				recorder,
			)

			err := controller.Sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())

		})
	}
}
