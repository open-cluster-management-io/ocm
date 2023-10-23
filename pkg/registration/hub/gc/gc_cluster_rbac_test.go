package gc

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/common/patcher"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestGCClusterRbacController(t *testing.T) {
	cases := []struct {
		name            string
		cluster         *clusterv1.ManagedCluster
		works           []*workv1.ManifestWork
		workRoleBinding runtime.Object
		expectedOp      gcReconcileOp
		validateActions func(t *testing.T, kubeActions, clusterActions []clienttesting.Action)
	}{
		{
			name:       "add finalizer to the mcl",
			cluster:    testinghelpers.NewManagedCluster(),
			expectedOp: gcReconcileStop,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, clusterActions, "patch")
				testingcommon.AssertNoActions(t, kubeActions)
			},
		},
		{
			name:    "delete rbac with no works",
			cluster: testinghelpers.NewDeletingManagedCluster(),
			workRoleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName,
				workRoleBindingName(testinghelpers.TestManagedClusterName), []string{manifestWorkFinalizer},
				map[string]string{clusterv1.ClusterNameLabelKey: testinghelpers.TestManagedClusterName}, true),
			expectedOp: gcReconcileContinue,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "delete", "delete", "delete", "delete", "patch")
				testingcommon.AssertActions(t, clusterActions, "patch")
			},
		},
		{
			name:    "delete rbac with works",
			cluster: testinghelpers.NewDeletingManagedCluster(),
			works: []*workv1.ManifestWork{
				testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName, "test", nil, nil, nil, nil),
			},
			workRoleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName,
				workRoleBindingName(testinghelpers.TestManagedClusterName), []string{manifestWorkFinalizer},
				map[string]string{clusterv1.ClusterNameLabelKey: testinghelpers.TestManagedClusterName}, true),

			expectedOp: gcReconcileRequeue,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "delete", "delete", "delete", "delete")
				testingcommon.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:       "remove finalizer of mcl",
			cluster:    testinghelpers.NewDeletingManagedCluster(),
			expectedOp: gcReconcileContinue,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "delete", "delete", "delete", "delete")
				testingcommon.AssertActions(t, clusterActions, "patch")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mclClusterRole := testinghelpers.NewClusterRole(
				mclClusterRoleName(c.cluster.Name),
				[]string{}, map[string]string{clusterv1.ClusterNameLabelKey: ""}, false)
			mclClusterRoleBinding := testinghelpers.NewClusterRoleBinding(
				mclClusterRoleBindingName(c.cluster.Name),
				[]string{}, map[string]string{clusterv1.ClusterNameLabelKey: ""}, false)
			registrationRoleBinding := testinghelpers.NewRoleBinding(c.cluster.Name,
				registrationRoleBindingName(c.cluster.Name),
				[]string{}, map[string]string{clusterv1.ClusterNameLabelKey: ""}, false)

			objs := []runtime.Object{mclClusterRole, mclClusterRoleBinding, registrationRoleBinding}
			if c.workRoleBinding != nil {
				objs = append(objs, c.workRoleBinding)
			}
			kubeClient := fakeclient.NewSimpleClientset(objs...)
			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)

			roleBindingStore := kubeInformerFactory.Rbac().V1().RoleBindings().Informer().GetStore()
			if c.workRoleBinding != nil {
				if err := roleBindingStore.Add(c.workRoleBinding); err != nil {
					t.Fatal(err)
				}
			}

			clusterClient := fakeclusterclient.NewSimpleClientset(c.cluster)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			if err := clusterStore.Add(c.cluster); err != nil {
				t.Fatal(err)
			}

			clusterPatcher := patcher.NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters())

			workClient := fakeworkclient.NewSimpleClientset()
			workInformerFactory := workinformers.NewSharedInformerFactory(workClient, 5*time.Minute)
			workStore := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
			for _, work := range c.works {
				if err := workStore.Add(work); err != nil {
					t.Fatal(err)
				}
			}

			_ = newGCClusterRbacController(
				kubeClient,
				clusterPatcher,
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				kubeInformerFactory.Rbac().V1().ClusterRoles().Lister(),
				kubeInformerFactory.Rbac().V1().ClusterRoleBindings().Lister(),
				kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				workInformerFactory.Work().V1().ManifestWorks().Lister(),
				events.NewInMemoryRecorder(""),
				true,
			)

			ctrl := &gcClusterRbacController{
				kubeClient:                       kubeClient,
				clusterLister:                    clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterRoleLister:                kubeInformerFactory.Rbac().V1().ClusterRoles().Lister(),
				clusterRoleBingLister:            kubeInformerFactory.Rbac().V1().ClusterRoleBindings().Lister(),
				roleBindingLister:                kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				manifestWorkLister:               workInformerFactory.Work().V1().ManifestWorks().Lister(),
				clusterPatcher:                   clusterPatcher,
				eventRecorder:                    events.NewInMemoryRecorder(""),
				resourceCleanupFeatureGateEnable: true,
			}
			op, err := ctrl.reconcile(context.TODO(), c.cluster)
			testingcommon.AssertError(t, err, "")
			assert.Equal(t, op, c.expectedOp)
			c.validateActions(t, kubeClient.Actions(), clusterClient.Actions())
		})
	}
}
