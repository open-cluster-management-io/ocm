package addontokeninfra

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

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
		},
	}
	addon.Status.KubeClientDriver = "token"
	return addon
}

func newAddonWithCSRRegistration(name, cluster string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, cluster)
	addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
		{
			SignerName: certificatesv1.KubeAPIServerClientSignerName,
		},
	}
	addon.Status.KubeClientDriver = "csr"
	return addon
}

func newAddonWithTokenInfraCondition(name, cluster string, status metav1.ConditionStatus) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, cluster)
	addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
		{
			SignerName: certificatesv1.KubeAPIServerClientSignerName,
		},
	}
	addon.Status.KubeClientDriver = "token"
	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    TokenInfrastructureReadyCondition,
		Status:  status,
		Reason:  "TokenInfrastructureReady",
		Message: "ServiceAccount cluster1/test-agent (UID: test-uid) is ready",
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

func TestTokenInfraResourceToAddonKey(t *testing.T) {
	cases := []struct {
		name     string
		obj      runtime.Object
		expected string
	}{
		{
			name: "serviceaccount with correct labels",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "true",
						"addon.open-cluster-management.io/name":                 "test",
					},
				},
			},
			expected: "cluster1/test",
		},
		{
			name: "role with correct labels",
			obj: &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-token-role",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "true",
						"addon.open-cluster-management.io/name":                 "test",
					},
				},
			},
			expected: "cluster1/test",
		},
		{
			name: "rolebinding with correct labels",
			obj: &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-token-role",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "true",
						"addon.open-cluster-management.io/name":                 "test",
					},
				},
			},
			expected: "cluster1/test",
		},
		{
			name: "resource without token-infrastructure label",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/name": "test",
					},
				},
			},
			expected: "",
		},
		{
			name: "resource with token-infrastructure=false",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "false",
						"addon.open-cluster-management.io/name":                 "test",
					},
				},
			},
			expected: "",
		},
		{
			name: "resource without addon name label",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "true",
					},
				},
			},
			expected: "",
		},
		{
			name: "resource without namespace",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-agent",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "true",
						"addon.open-cluster-management.io/name":                 "test",
					},
				},
			},
			expected: "",
		},
		{
			name: "resource without any labels",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
				},
			},
			expected: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := tokenInfraResourceToAddonKey(c.obj)
			if result != c.expected {
				t.Errorf("expected %q, got %q", c.expected, result)
			}
		})
	}
}

func TestEventHandler(t *testing.T) {
	cases := []struct {
		name        string
		eventType   string
		obj         interface{}
		oldObj      interface{}
		expectQueue bool
		expectError bool
	}{
		{
			name:      "update with valid resource",
			eventType: "update",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "true",
						"addon.open-cluster-management.io/name":                 "test",
					},
				},
			},
			oldObj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
				},
			},
			expectQueue: true,
			expectError: false,
		},
		{
			name:      "delete with valid resource",
			eventType: "delete",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "cluster1",
					Labels: map[string]string{
						"addon.open-cluster-management.io/token-infrastructure": "true",
						"addon.open-cluster-management.io/name":                 "test",
					},
				},
			},
			expectQueue: true,
			expectError: false,
		},
		{
			name:      "delete with tombstone",
			eventType: "delete",
			obj: cache.DeletedFinalStateUnknown{
				Key: "cluster1/test-agent",
				Obj: &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-agent",
						Namespace: "cluster1",
						Labels: map[string]string{
							"addon.open-cluster-management.io/token-infrastructure": "true",
							"addon.open-cluster-management.io/name":                 "test",
						},
					},
				},
			},
			expectQueue: true,
			expectError: false,
		},
		{
			name:        "update with invalid type",
			eventType:   "update",
			obj:         "not-a-runtime-object",
			oldObj:      &corev1.ServiceAccount{},
			expectQueue: false,
			expectError: true,
		},
		{
			name:        "delete with invalid type",
			eventType:   "delete",
			obj:         "not-a-runtime-object",
			expectQueue: false,
			expectError: true,
		},
		{
			name:      "delete with invalid tombstone",
			eventType: "delete",
			obj: cache.DeletedFinalStateUnknown{
				Key: "cluster1/test-agent",
				Obj: "not-a-runtime-object",
			},
			expectQueue: false,
			expectError: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			syncCtx := testingcommon.NewFakeSyncContext(t, "test-key")
			handler := newTokenInfraEventHandler(syncCtx, tokenInfraResourceToAddonKey)

			// Capture runtime errors
			errorCaptured := false
			utilruntime.ErrorHandlers = []utilruntime.ErrorHandler{
				func(ctx context.Context, err error, msg string, keysAndValues ...interface{}) {
					errorCaptured = true
				},
			}

			switch c.eventType {
			case "update":
				handler.OnUpdate(c.oldObj, c.obj)
			case "delete":
				handler.OnDelete(c.obj)
			}

			if c.expectError && !errorCaptured {
				t.Errorf("expected error to be captured but got none")
			}
			if !c.expectError && errorCaptured {
				t.Errorf("unexpected error was captured")
			}

			queueLen := syncCtx.Queue().Len()
			if c.expectQueue && queueLen == 0 {
				t.Errorf("expected item in queue but queue is empty")
			}
			if !c.expectQueue && queueLen > 0 {
				t.Errorf("expected empty queue but got %d items", queueLen)
			}

			// Drain the queue
			for syncCtx.Queue().Len() > 0 {
				item, _ := syncCtx.Queue().Get()
				syncCtx.Queue().Done(item)
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
				// Should attempt cleanup even though addon doesn't exist
				deleteCount := 0
				for _, action := range actions {
					if action.GetVerb() == "delete" {
						deleteCount++
					}
				}
				if deleteCount != 3 {
					t.Errorf("expected 3 delete actions for cleanup, got %d", deleteCount)
				}
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
				testingcommon.AssertActions(t, actions, "patch")
				patchAction := actions[0].(clienttesting.PatchActionImpl)
				addon := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patchAction.Patch, addon)
				if err != nil {
					t.Fatal(err)
				}
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
						Message: "ServiceAccount cluster1/test-agent (UID: test-uid) is ready",
					})
					return addon
				}(),
			},
			kubeObjects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-agent",
						Namespace: "cluster1",
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-token-role",
						Namespace: "cluster1",
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-token-role",
						Namespace: "cluster1",
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patchAction := actions[0].(clienttesting.PatchActionImpl)
				addon := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patchAction.Patch, addon)
				if err != nil {
					t.Fatal(err)
				}
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
						Name:      "test-agent",
						Namespace: "cluster1",
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-token-role",
						Namespace: "cluster1",
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-token-role",
						Namespace: "cluster1",
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// No addon actions expected - the addon is being deleted, no need to update condition
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should delete RoleBinding, Role, and ServiceAccount
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
			name:    "update condition when infrastructure already exists",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				newAddonWithTokenRegistration("test", "cluster1"),
			},
			kubeObjects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-agent",
						Namespace: "cluster1",
						UID:       types.UID("test-uid"),
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-token-role",
						Namespace: "cluster1",
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-token-role",
						Namespace: "cluster1",
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patchAction := actions[0].(clienttesting.PatchActionImpl)
				addon := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patchAction.Patch, addon)
				if err != nil {
					t.Fatal(err)
				}
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
		{
			name:    "addon being deleted without TokenInfrastructureReady condition",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := newAddonWithTokenRegistration("test", "cluster1")
					now := metav1.Now()
					addon.DeletionTimestamp = &now
					return addon
				}(),
			},
			kubeObjects: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// No condition to remove, should be no-op
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				// No infrastructure to clean up
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:    "addon with multiple registrations, only one token-based",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.Registrations = []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
						},
						{
							SignerName: "example.com/custom-signer",
						},
					}
					addon.Status.KubeClientDriver = "token"
					return addon
				}(),
			},
			kubeObjects: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should create infrastructure and set condition
				testingcommon.AssertActions(t, actions, "patch")
				patchAction := actions[0].(clienttesting.PatchActionImpl)
				addon := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patchAction.Patch, addon)
				if err != nil {
					t.Fatal(err)
				}
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
			name:    "invalid sync key",
			syncKey: "invalid-key-without-slash",
			managedClusterAddon: []runtime.Object{
				newAddonWithTokenRegistration("test", "cluster1"),
			},
			kubeObjects: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should attempt cleanup even with malformed key (SplitMetaNamespaceKey doesn't error on this)
				deleteCount := 0
				for _, action := range actions {
					if action.GetVerb() == "delete" {
						deleteCount++
					}
				}
				if deleteCount != 3 {
					t.Errorf("expected 3 delete actions for cleanup, got %d", deleteCount)
				}
			},
		},
		{
			name:    "condition transition from False to True after recovery",
			syncKey: "cluster1/test",
			managedClusterAddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := newAddonWithTokenRegistration("test", "cluster1")
					meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
						Type:    TokenInfrastructureReadyCondition,
						Status:  metav1.ConditionFalse,
						Reason:  "TokenInfrastructureApplyFailed",
						Message: "Failed to apply token infrastructure",
					})
					return addon
				}(),
			},
			kubeObjects: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patchAction := actions[0].(clienttesting.PatchActionImpl)
				addon := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patchAction.Patch, addon)
				if err != nil {
					t.Fatal(err)
				}
				cond := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
				if cond == nil {
					t.Errorf("TokenInfrastructureReady condition not found")
					return
				}
				if cond.Status != metav1.ConditionTrue {
					t.Errorf("expected condition status True after recovery, got %s", cond.Status)
				}
				if cond.Reason != "TokenInfrastructureReady" {
					t.Errorf("expected reason TokenInfrastructureReady, got %s", cond.Reason)
				}
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
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
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append([]runtime.Object{}, c.managedClusterAddon...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)
			fakeKubeClient := kubefake.NewSimpleClientset(c.kubeObjects...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			kubeInformers := informers.NewSharedInformerFactory(fakeKubeClient, 10*time.Minute)

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
				kubeInformers.Core().V1().ServiceAccounts(),
				kubeInformers.Rbac().V1().Roles(),
				kubeInformers.Rbac().V1().RoleBindings(),
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
