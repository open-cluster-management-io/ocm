package clusterrole

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"

	"open-cluster-management.io/ocm/pkg/common/apply"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSyncManagedClusterClusterRole(t *testing.T) {
	const (
		testCustomLabel      = "custom-label"
		testCustomLabelValue = "custom-value"
	)
	cases := []struct {
		name            string
		clusters        []runtime.Object
		clusterroles    []runtime.Object
		labels          map[string]string
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:         "create clusterroles",
			clusters:     []runtime.Object{testinghelpers.NewManagedCluster()},
			clusterroles: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create")
				registrationClusterRole := (actions[0].(clienttesting.CreateActionImpl).Object).(*rbacv1.ClusterRole)
				if registrationClusterRole.Name != "open-cluster-management:managedcluster:registration" {
					t.Errorf("expected registration clusterrole, but failed")
				}
				workClusterRole := (actions[1].(clienttesting.CreateActionImpl).Object).(*rbacv1.ClusterRole)
				if workClusterRole.Name != "open-cluster-management:managedcluster:work" {
					t.Errorf("expected work clusterrole, but failed")
				}
			},
		},
		{
			name:     "delete clusterroles",
			clusters: []runtime.Object{},
			clusterroles: []runtime.Object{
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "open-cluster-management:managedcluster:registration"}},
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "open-cluster-management:managedcluster:work"}},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete", "delete")
				if actions[0].(clienttesting.DeleteActionImpl).Name != "open-cluster-management:managedcluster:registration" {
					t.Errorf("expected registration clusterrole, but failed")
				}
				if actions[1].(clienttesting.DeleteActionImpl).Name != "open-cluster-management:managedcluster:work" {
					t.Errorf("expected work clusterrole, but failed")
				}
			},
		},
		{
			name:         "add labels to created clusterroles",
			clusters:     []runtime.Object{testinghelpers.NewManagedCluster()},
			clusterroles: []runtime.Object{},
			labels:       map[string]string{testCustomLabel: testCustomLabelValue},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create")
				registrationClusterRole := (actions[0].(clienttesting.CreateActionImpl).Object).(*rbacv1.ClusterRole)
				if registrationClusterRole.Name != "open-cluster-management:managedcluster:registration" {
					t.Errorf("expected registration clusterrole, but failed")
				}
				if registrationClusterRole.Labels[testCustomLabel] != testCustomLabelValue {
					t.Errorf("expected label '%s=%s' on registration clusterrole, but not found", testCustomLabel, testCustomLabelValue)
				}

				workClusterRole := (actions[1].(clienttesting.CreateActionImpl).Object).(*rbacv1.ClusterRole)
				if workClusterRole.Name != "open-cluster-management:managedcluster:work" {
					t.Errorf("expected work clusterrole, but failed")
				}
				if workClusterRole.Labels[testCustomLabel] != testCustomLabelValue {
					t.Errorf("expected label '%s=%s' on work clusterrole, but not found", testCustomLabel, testCustomLabelValue)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.clusterroles...)
			kubeInformer := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, time.Minute*10)

			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &clusterroleController{
				kubeClient: kubeClient,
				applier: apply.NewPermissionApplier(
					kubeClient,
					nil,
					nil,
					kubeInformer.Rbac().V1().ClusterRoles().Lister(),
					nil,
				),
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				cache:         resourceapply.NewResourceCache(),
				labels:        c.labels,
			}

			syncErr := ctrl.sync(context.TODO(),
				testingcommon.NewFakeSyncContext(t, "testmangedclsuterclusterrole"),
				"testmangedclsuterclusterrole")
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, kubeClient.Actions())
		})
	}
}
