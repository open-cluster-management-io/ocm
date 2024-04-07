package managedcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/apply"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSyncManagedCluster(t *testing.T) {
	cases := []struct {
		name                string
		autoApprovalEnabled bool
		startingObjects     []runtime.Object
		validateActions     func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "sync a deleted spoke cluster",
			startingObjects: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:            "create a new spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:            "accept a spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptingManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
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
		},
		{
			name:            "sync an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:            "deny an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewDeniedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
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
		},
		{
			name:            "delete a spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:                "should accept the clusters when auto approval is enabled",
			autoApprovalEnabled: true,
			startingObjects:     []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
	}

	features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			kubeClient := kubefake.NewSimpleClientset()
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			kubeInformer := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.startingObjects {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			features.HubMutableFeatureGate.Set(fmt.Sprintf("%s=%v", ocmfeature.ManagedClusterAutoApproval, c.autoApprovalEnabled))

			ctrl := managedClusterController{
				kubeClient,
				clusterClient,
				clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				apply.NewPermissionApplier(
					kubeClient,
					kubeInformer.Rbac().V1().Roles().Lister(),
					kubeInformer.Rbac().V1().RoleBindings().Lister(),
					kubeInformer.Rbac().V1().ClusterRoles().Lister(),
					kubeInformer.Rbac().V1().ClusterRoleBindings().Lister(),
				),
				patcher.NewPatcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus](clusterClient.ClusterV1().ManagedClusters()),
				eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
