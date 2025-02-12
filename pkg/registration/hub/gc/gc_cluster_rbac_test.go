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
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

func TestGCClusterRbacController(t *testing.T) {
	cases := []struct {
		name                             string
		clusterName                      string
		cluster                          *clusterv1.ManagedCluster
		works                            []*workv1.ManifestWork
		workRoleBinding                  runtime.Object
		resourceCleanupFeatureGateEnable bool
		expectedOp                       gcReconcileOp
		validateActions                  func(t *testing.T, kubeActions, clusterActions []clienttesting.Action)
	}{
		{
			name:       "no cluster and work, do nothing",
			expectedOp: gcReconcileStop,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, kubeActions)
			},
		},
		{
			name:        "cluster is nil and have works, Requeue",
			clusterName: "cluster1",
			works: []*workv1.ManifestWork{
				testinghelpers.NewManifestWork("cluster1", "test", nil, nil, nil, nil),
			},
			expectedOp: gcReconcileRequeue,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, kubeActions)
			},
		},
		{
			name:        "cluster is nil and no works, remove finalizer from work rolebinding",
			clusterName: "cluster1",
			expectedOp:  gcReconcileStop,
			workRoleBinding: testinghelpers.NewRoleBinding("cluster1",
				workRoleBindingName("cluster1"), []string{manifestWorkFinalizer},
				map[string]string{clusterv1.ClusterNameLabelKey: "cluster1"}, true),
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "patch")
			},
		},
		{
			name:                             "gc is disable,  delete rbac and remove finalizer from cluster",
			clusterName:                      testinghelpers.TestManagedClusterName,
			cluster:                          testinghelpers.NewDeletingManagedCluster(),
			resourceCleanupFeatureGateEnable: false,
			expectedOp:                       gcReconcileStop,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "delete", "delete", "delete", "delete")
				testingcommon.AssertActions(t, clusterActions, "patch")
			},
		},
		{
			name:                             "gc is enable with no work, delete rbac ",
			clusterName:                      testinghelpers.TestManagedClusterName,
			cluster:                          testinghelpers.NewDeletingManagedCluster(),
			resourceCleanupFeatureGateEnable: true,
			expectedOp:                       gcReconcileStop,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "delete", "delete", "delete", "delete")
			},
		},
		{
			name:                             "gc is disable with works",
			clusterName:                      testinghelpers.TestManagedClusterName,
			cluster:                          testinghelpers.NewDeletingManagedCluster(),
			resourceCleanupFeatureGateEnable: false,
			works: []*workv1.ManifestWork{
				testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName, "test", nil, nil, nil, nil),
			},
			workRoleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName,
				workRoleBindingName(testinghelpers.TestManagedClusterName), []string{manifestWorkFinalizer},
				map[string]string{clusterv1.ClusterNameLabelKey: testinghelpers.TestManagedClusterName}, true),

			expectedOp: gcReconcileRequeue,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "delete", "delete", "delete", "delete")
				testingcommon.AssertActions(t, clusterActions, "patch")
			},
		},
		{
			name:                             "gc is disable with no works",
			clusterName:                      testinghelpers.TestManagedClusterName,
			cluster:                          testinghelpers.NewDeletingManagedCluster(),
			resourceCleanupFeatureGateEnable: false,
			workRoleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName,
				workRoleBindingName(testinghelpers.TestManagedClusterName), []string{manifestWorkFinalizer},
				map[string]string{clusterv1.ClusterNameLabelKey: testinghelpers.TestManagedClusterName}, true),

			expectedOp: gcReconcileStop,
			validateActions: func(t *testing.T, kubeActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "delete", "delete", "delete", "delete", "patch")
				testingcommon.AssertActions(t, clusterActions, "patch")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objs := []runtime.Object{}

			mclClusterRole := testinghelpers.NewClusterRole(
				mclClusterRoleName(c.clusterName),
				[]string{}, map[string]string{clusterv1.ClusterNameLabelKey: ""}, false)
			mclClusterRoleBinding := testinghelpers.NewClusterRoleBinding(
				mclClusterRoleBindingName(c.clusterName),
				[]string{}, map[string]string{clusterv1.ClusterNameLabelKey: ""}, false)

			registrationRoleBinding := testinghelpers.NewRoleBinding(c.clusterName,
				registrationRoleBindingName(c.clusterName),
				[]string{}, map[string]string{clusterv1.ClusterNameLabelKey: ""}, false)
			objs = append(objs, mclClusterRole, mclClusterRoleBinding, registrationRoleBinding)

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

			var clusterPatcher patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
			var clusterClient *fakeclusterclient.Clientset
			if c.cluster != nil {
				clusterClient = fakeclusterclient.NewSimpleClientset(c.cluster)
				clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
				clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
				if err := clusterStore.Add(c.cluster); err != nil {
					t.Fatal(err)
				}

				clusterPatcher = patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters())
			}

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
				kubeInformerFactory.Rbac().V1().ClusterRoles().Lister(),
				kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				workInformerFactory.Work().V1().ManifestWorks().Lister(),
				register.NewNoopHubDriver(),
				events.NewInMemoryRecorder(""),
				c.resourceCleanupFeatureGateEnable,
			)

			ctrl := &gcClusterRbacController{
				kubeClient:                       kubeClient,
				clusterRoleLister:                kubeInformerFactory.Rbac().V1().ClusterRoles().Lister(),
				roleBindingLister:                kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				manifestWorkLister:               workInformerFactory.Work().V1().ManifestWorks().Lister(),
				clusterPatcher:                   clusterPatcher,
				hubDriver:                         register.NewNoopHubDriver(),
				eventRecorder:                    events.NewInMemoryRecorder(""),
				resourceCleanupFeatureGateEnable: c.resourceCleanupFeatureGateEnable,
			}
			op, err := ctrl.reconcile(context.TODO(), c.cluster, c.clusterName)
			testingcommon.AssertError(t, err, "")
			assert.Equal(t, op, c.expectedOp)
			if clusterClient != nil {
				c.validateActions(t, kubeClient.Actions(), clusterClient.Actions())
			} else {
				c.validateActions(t, kubeClient.Actions(), nil)
			}
		})
	}
}
