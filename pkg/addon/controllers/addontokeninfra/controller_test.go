package addontokeninfra

import (
	"context"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func newAddonWithTokenRegistration(name, cluster string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, cluster)
	addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
		{
			SignerName: certificatesv1.KubeAPIServerClientSignerName,
			Driver:     "token",
		},
	}
	return addon
}

func newAddonWithCSRRegistration(name, cluster string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, cluster)
	addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
		{
			SignerName: certificatesv1.KubeAPIServerClientSignerName,
			Driver:     "csr",
		},
	}
	return addon
}

func newAddonWithTokenInfraCondition(name, cluster string, status metav1.ConditionStatus) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, cluster)
	addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
		{
			SignerName: certificatesv1.KubeAPIServerClientSignerName,
			Driver:     "token",
		},
	}
	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    TokenInfrastructureReadyCondition,
		Status:  status,
		Reason:  "TokenInfrastructureReady",
		Message: "ServiceAccount cluster1/cluster1-test-agent (UID: test-uid) is ready",
	})
	return addon
}

func TestAddonFilter(t *testing.T) {
	cases := []struct {
		name     string
		addon    interface{}
		expected bool
	}{
		{
			name:     "not an addon object",
			addon:    &corev1.Pod{},
			expected: false,
		},
		{
			name:     "addon with token driver",
			addon:    newAddonWithTokenRegistration("test", "cluster1"),
			expected: true,
		},
		{
			name:     "addon with CSR driver",
			addon:    newAddonWithCSRRegistration("test", "cluster1"),
			expected: false,
		},
		{
			name:     "addon with TokenInfrastructureReady condition",
			addon:    newAddonWithTokenInfraCondition("test", "cluster1", metav1.ConditionTrue),
			expected: true,
		},
		{
			name:     "addon without token driver or condition",
			addon:    addontesting.NewAddon("test", "cluster1"),
			expected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := addonFilter(c.addon)
			if result != c.expected {
				t.Errorf("expected %v, got %v", c.expected, result)
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		syncKey              string
		managedClusterAddon  []runtime.Object
		kubeObjects          []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
		validateKubeActions  func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                "no addon",
			syncKey:             "cluster1/test",
			managedClusterAddon: []runtime.Object{},
			kubeObjects:         []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:    "addon without token driver",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				newAddonWithCSRRegistration("test", "cluster1"),
			},
			kubeObjects: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:    "create token infrastructure for addon with token driver",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				newAddonWithTokenRegistration("test", "cluster1"),
			},
			kubeObjects: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				updateAction := actions[0].(clienttesting.UpdateActionImpl)
				addon := updateAction.Object.(*addonapiv1alpha1.ManagedClusterAddOn)
				cond := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
				if cond == nil {
					t.Errorf("TokenInfrastructureReady condition not found")
					return
				}
				if cond.Status != metav1.ConditionTrue {
					t.Errorf("expected condition status True, got %s", cond.Status)
				}
				if cond.Reason != "TokenInfrastructureReady" {
					t.Errorf("expected reason TokenInfrastructureReady, got %s", cond.Reason)
				}
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should create ServiceAccount, Role, and RoleBinding
				// resourceapply.ApplyDirectly also does gets, so we check for creates
				createCount := 0
				for _, action := range actions {
					if action.GetVerb() == "create" {
						createCount++
					}
				}
				if createCount != 3 {
					t.Errorf("expected 3 create actions, got %d", createCount)
				}
			},
		},
		{
			name:    "cleanup when addon switches from token to CSR",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := newAddonWithCSRRegistration("test", "cluster1")
					meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
						Type:    TokenInfrastructureReadyCondition,
						Status:  metav1.ConditionTrue,
						Reason:  "TokenInfrastructureReady",
						Message: "ServiceAccount cluster1/cluster1-test-agent (UID: test-uid) is ready",
					})
					return addon
				}(),
			},
			kubeObjects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-agent",
						Namespace: "cluster1",
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-token-role",
						Namespace: "cluster1",
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-token-rolebinding",
						Namespace: "cluster1",
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				updateAction := actions[0].(clienttesting.UpdateActionImpl)
				addon := updateAction.Object.(*addonapiv1alpha1.ManagedClusterAddOn)
				cond := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
				if cond != nil {
					t.Errorf("TokenInfrastructureReady condition should be removed")
				}
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should delete RoleBinding, Role, and ServiceAccount
				// resourceapply.DeleteAll also does gets, so we check for deletes
				deleteCount := 0
				for _, action := range actions {
					if action.GetVerb() == "delete" {
						deleteCount++
					}
				}
				if deleteCount != 3 {
					t.Errorf("expected 3 delete actions, got %d", deleteCount)
				}
			},
		},
		{
			name:    "cleanup when addon is being deleted",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := newAddonWithTokenInfraCondition("test", "cluster1", metav1.ConditionTrue)
					now := metav1.Now()
					addon.DeletionTimestamp = &now
					return addon
				}(),
			},
			kubeObjects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-agent",
						Namespace: "cluster1",
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-token-role",
						Namespace: "cluster1",
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-token-rolebinding",
						Namespace: "cluster1",
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				updateAction := actions[0].(clienttesting.UpdateActionImpl)
				addon := updateAction.Object.(*addonapiv1alpha1.ManagedClusterAddOn)
				cond := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
				if cond != nil {
					t.Errorf("TokenInfrastructureReady condition should be removed")
				}
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should delete RoleBinding, Role, and ServiceAccount
				expectedActions := []string{"delete", "delete", "delete"}
				if len(actions) != len(expectedActions) {
					t.Errorf("expected %d actions, got %d", len(expectedActions), len(actions))
					return
				}
			},
		},
		{
			name:    "update condition when infrastructure already exists",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				newAddonWithTokenRegistration("test", "cluster1"),
			},
			kubeObjects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-agent",
						Namespace: "cluster1",
						UID:       types.UID("test-uid"),
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-token-role",
						Namespace: "cluster1",
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-test-token-rolebinding",
						Namespace: "cluster1",
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				updateAction := actions[0].(clienttesting.UpdateActionImpl)
				addon := updateAction.Object.(*addonapiv1alpha1.ManagedClusterAddOn)
				cond := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
				if cond == nil {
					t.Errorf("TokenInfrastructureReady condition not found")
					return
				}
				if cond.Status != metav1.ConditionTrue {
					t.Errorf("expected condition status True, got %s", cond.Status)
				}
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should update existing resources
				if len(actions) == 0 {
					t.Errorf("expected some actions")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append([]runtime.Object{}, c.managedClusterAddon...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)
			fakeKubeClient := kubefake.NewSimpleClientset(c.kubeObjects...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

			for _, obj := range c.managedClusterAddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)

			controller := NewTokenInfrastructureController(
				fakeKubeClient,
				fakeAddonClient,
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			)

			err := controller.Sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}

			c.validateAddonActions(t, fakeAddonClient.Actions())
			c.validateKubeActions(t, fakeKubeClient.Actions())
		})
	}
}
