package managedcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/apply"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

func TestSyncManagedCluster(t *testing.T) {
	const (
		testCustomLabel       = "custom-label"
		testCustomLabelValue  = "custom-value"
		testCustomLabel2      = "custom-label2"
		testCustomLabelValue2 = "custom-value2"
	)
	cases := []struct {
		name                   string
		autoApprovalEnabled    bool
		roleBindings           []runtime.Object
		manifestWorks          []runtime.Object
		startingObjects        []runtime.Object
		labels                 map[string]string
		validateClusterActions func(t *testing.T, actions []clienttesting.Action)
		validateKubeActions    func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "sync a deleted spoke cluster",
			startingObjects: []runtime.Object{},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions,
					"delete", // clusterrole
					"delete", // clusterrolebinding
					"delete", // registration rolebinding
					"delete") // work rolebinding
			},
		},
		{
			name:            "create a new spoke cluster(not accepted before, no accept condition)",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:            "accept a spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptingManagedCluster()},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    v1.ManagedClusterConditionHubAccepted,
					Status:  metav1.ConditionTrue,
					Reason:  "HubClusterAdminAccepted",
					Message: "Accepted by hub cluster admin",
				}
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions,
					"get", "create", // namespace
					"create", // clusterrole
					"create", // clusterrolebinding
					"create", // registration rolebinding
					"create") // work rolebinding
			},
		},
		{
			name:            "sync an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions,
					"get", "create", // namespace
					"create", // clusterrole
					"create", // clusterrolebinding
					"create", // registration rolebinding
					"create") // work rolebinding
			},
		},
		{
			name:            "deny an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewDeniedManagedCluster("True")},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    v1.ManagedClusterConditionHubAccepted,
					Status:  metav1.ConditionFalse,
					Reason:  "HubClusterAdminDenied",
					Message: "Denied by hub cluster admin",
				}
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions,
					"create", // clusterrole
					"create", // clusterrolebinding
					"delete", // registration rolebinding
					"delete") // work rolebinding
			},
		},
		{
			name: "delete a spoke cluster without manifestworks",
			roleBindings: []runtime.Object{testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName,
				workRoleBindingName(testinghelpers.TestManagedClusterName), []string{workv1.ManifestWorkFinalizer},
				nil, false)},
			startingObjects: []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if len(managedCluster.Finalizers) != 0 {
					t.Errorf("expected no finalizer")
				}
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions,
					"delete", // clusterrole
					"delete", // clusterrolebinding
					"delete", // registration rolebinding
					"delete", // work rolebinding
					"patch")  // work rolebinding
				patch := actions[4].(clienttesting.PatchAction).GetPatch()
				roleBinding := &rbacv1.RoleBinding{}
				err := json.Unmarshal(patch, roleBinding)
				if err != nil {
					t.Fatal(err)
				}
				if len(roleBinding.Finalizers) != 0 {
					t.Errorf("expected no finalizer")
				}
			},
		},
		{
			name:            "delete a spoke cluster with manifestworks",
			startingObjects: []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			manifestWorks: []runtime.Object{testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName,
				"test", nil, nil, nil, nil)},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions,
					"delete", // clusterrole
					"delete", // clusterrolebinding
					"delete", // registration rolebinding
					"delete") // work rolebinding
			},
		},
		{
			name:                "should accept the clusters when auto approval is enabled",
			autoApprovalEnabled: true,
			startingObjects:     []runtime.Object{testinghelpers.NewManagedCluster()},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
		{
			name:                "should add the auto approval annotation to an accepted cluster when auto approval is enabled",
			autoApprovalEnabled: true,
			startingObjects:     []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := managedCluster.Annotations[clusterAcceptedAnnotationKey]; !ok {
					t.Errorf("expected auto approval annotation, but failed")
				}
			},
		},
		{
			name:            "create resources with labels",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateClusterActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
			labels: map[string]string{testCustomLabel: testCustomLabelValue, testCustomLabel2: testCustomLabelValue2},
			validateKubeActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions,
					"get", "create", // namespace
					"create", // clusterrole
					"create", // clusterrolebinding
					"create", // registration rolebinding
					"create") // work rolebinding

				// Validate labels on namespace
				namespace := actions[1].(clienttesting.CreateAction).GetObject().(*corev1.Namespace)
				if namespace.Labels[testCustomLabel] != testCustomLabelValue {
					t.Errorf("expected label '%s=%s' on namespace, but got: %v", testCustomLabel, testCustomLabelValue, namespace.Labels)
				}
				if namespace.Labels[testCustomLabel2] != testCustomLabelValue2 {
					t.Errorf("expected label '%s=%s' on namespace, but got: %v", testCustomLabel2, testCustomLabelValue2, namespace.Labels)
				}

				// Validate labels on clusterrole
				clusterRole := actions[2].(clienttesting.CreateAction).GetObject().(*rbacv1.ClusterRole)
				if clusterRole.Labels[testCustomLabel] != testCustomLabelValue {
					t.Errorf("expected label '%s=%s' on clusterrole, but got: %v", testCustomLabel, testCustomLabelValue, clusterRole.Labels)
				}
				if clusterRole.Labels[testCustomLabel2] != testCustomLabelValue2 {
					t.Errorf("expected label '%s=%s' on clusterrole, but got: %v", testCustomLabel2, testCustomLabelValue2, clusterRole.Labels)
				}

				// Validate labels on clusterrolebinding
				clusterRoleBinding := actions[3].(clienttesting.CreateAction).GetObject().(*rbacv1.ClusterRoleBinding)
				if clusterRoleBinding.Labels[testCustomLabel] != testCustomLabelValue {
					t.Errorf("expected label '%s=%s' on clusterrolebinding, but got: %v", testCustomLabel, testCustomLabelValue, clusterRoleBinding.Labels)
				}
				if clusterRoleBinding.Labels[testCustomLabel2] != testCustomLabelValue2 {
					t.Errorf("expected label '%s=%s' on clusterrolebinding, but got: %v", testCustomLabel2, testCustomLabelValue2, clusterRoleBinding.Labels)
				}

				// Validate labels on registration rolebinding
				registrationRoleBinding := actions[4].(clienttesting.CreateAction).GetObject().(*rbacv1.RoleBinding)
				if registrationRoleBinding.Labels[testCustomLabel] != testCustomLabelValue {
					t.Errorf("expected label '%s=%s' on registration rolebinding, but got: %v", testCustomLabel, testCustomLabelValue, registrationRoleBinding.Labels)
				}
				if registrationRoleBinding.Labels[testCustomLabel2] != testCustomLabelValue2 {
					t.Errorf("expected label '%s=%s' on registration rolebinding, but got: %v", testCustomLabel2, testCustomLabelValue2, registrationRoleBinding.Labels)
				}

				// Validate labels on work rolebinding
				workRoleBinding := actions[5].(clienttesting.CreateAction).GetObject().(*rbacv1.RoleBinding)
				if workRoleBinding.Labels[testCustomLabel] != testCustomLabelValue {
					t.Errorf("expected label '%s=%s' on work rolebinding, but got: %v", testCustomLabel, testCustomLabelValue, workRoleBinding.Labels)
				}
				if workRoleBinding.Labels[testCustomLabel2] != testCustomLabelValue2 {
					t.Errorf("expected label '%s=%s' on work rolebinding, but got: %v", testCustomLabel2, testCustomLabelValue2, workRoleBinding.Labels)
				}
			},
		},
	}

	features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			kubeClient := kubefake.NewSimpleClientset(c.roleBindings...)

			kubeInformer := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, time.Minute*10)
			roleBindingStore := kubeInformer.Rbac().V1().RoleBindings().Informer().GetStore()
			for _, roleBinding := range c.roleBindings {
				if err := roleBindingStore.Add(roleBinding); err != nil {
					t.Fatal(err)
				}
			}

			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.startingObjects {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			workClient := fakeworkclient.NewSimpleClientset(c.manifestWorks...)
			workInformerFactory := workinformers.NewSharedInformerFactory(workClient, time.Minute*10)
			workStore := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
			for _, work := range c.manifestWorks {
				if err := workStore.Add(work); err != nil {
					t.Fatal(err)
				}
			}

			features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=%v", ocmfeature.ManagedClusterAutoApproval, c.autoApprovalEnabled))
			ctrl := managedClusterController{
				kubeClient,
				clusterClient,
				kubeInformer.Rbac().V1().RoleBindings().Lister(),
				workInformerFactory.Work().V1().ManifestWorks().Lister(),
				clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				apply.NewPermissionApplier(
					kubeClient,
					kubeInformer.Rbac().V1().Roles().Lister(),
					kubeInformer.Rbac().V1().RoleBindings().Lister(),
					kubeInformer.Rbac().V1().ClusterRoles().Lister(),
					kubeInformer.Rbac().V1().ClusterRoleBindings().Lister(),
				),
				patcher.NewPatcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus](clusterClient.ClusterV1().ManagedClusters()),
				register.NewNoopHubDriver(),
				c.labels}
			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName), testinghelpers.TestManagedClusterName)
			if syncErr != nil && !errors.Is(syncErr, requeueError) {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateClusterActions(t, clusterClient.Actions())
			if c.validateKubeActions != nil {
				c.validateKubeActions(t, kubeClient.Actions())
			}
		})
	}
}
