package clusterrole

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestSyncManagedClusterClusterRole(t *testing.T) {
	cases := []struct {
		name            string
		clusters        []runtime.Object
		clusterroles    []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:         "create clusterroles",
			clusters:     []runtime.Object{testinghelpers.NewManagedCluster()},
			clusterroles: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "create", "get", "create")
				registrationClusterRole := (actions[1].(clienttesting.CreateActionImpl).Object).(*rbacv1.ClusterRole)
				if registrationClusterRole.Name != "open-cluster-management:managedcluster:registration" {
					t.Errorf("expected registration clusterrole, but failed")
				}
				workClusterRole := (actions[3].(clienttesting.CreateActionImpl).Object).(*rbacv1.ClusterRole)
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
				testinghelpers.AssertActions(t, actions, "delete", "delete")
				if actions[0].(clienttesting.DeleteActionImpl).Name != "open-cluster-management:managedcluster:registration" {
					t.Errorf("expected registration clusterrole, but failed")
				}
				if actions[1].(clienttesting.DeleteActionImpl).Name != "open-cluster-management:managedcluster:work" {
					t.Errorf("expected work clusterrole, but failed")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.clusterroles...)

			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &clusterroleController{
				kubeClient:    kubeClient,
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				cache:         resourceapply.NewResourceCache(),
				eventRecorder: eventstesting.NewTestingEventRecorder(t),
			}

			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, "testmangedclsuterclusterrole"))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, kubeClient.Actions())
		})
	}
}
