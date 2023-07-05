package rbacfinalizerdeletion

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

var roleName = fmt.Sprintf("%s:spoke-work", testinghelpers.TestManagedClusterName)

func TestSync(t *testing.T) {
	cases := []struct {
		name         string
		key          string
		clusters     []runtime.Object
		namespaces   []runtime.Object
		roleBindings []runtime.Object
		works        []runtime.Object
		expectedErr  string
	}{
		{
			name:        "key is empty",
			key:         "",
			expectedErr: "",
		},
		{
			name:        "managed cluster namespace is not found",
			key:         testinghelpers.TestManagedClusterName,
			expectedErr: "",
		},

		{
			name:        "there are no resources in managed cluster namespace",
			key:         testinghelpers.TestManagedClusterName,
			namespaces:  []runtime.Object{testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, true)},
			expectedErr: "",
		},

		{
			name:        "cluster and ns are not deleting",
			key:         testinghelpers.TestManagedClusterName,
			namespaces:  []runtime.Object{testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, false)},
			clusters:    []runtime.Object{testinghelpers.NewManagedCluster()},
			expectedErr: "",
		},
		{
			name:       "still have works in deleting managed cluster namespace",
			key:        testinghelpers.TestManagedClusterName,
			namespaces: []runtime.Object{testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, true)},
			roleBindings: []runtime.Object{testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName,
				[]string{manifestWorkFinalizer}, map[string]string{clusterv1.ClusterNameLabelKey: testinghelpers.TestManagedClusterName}, true)},
			works:       []runtime.Object{testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName, "work1", []string{manifestWorkFinalizer}, nil)},
			expectedErr: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakeclient.NewSimpleClientset()
			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)
			nsStore := kubeInformerFactory.Core().V1().Namespaces().Informer().GetStore()
			for _, ns := range c.namespaces {
				if err := nsStore.Add(ns); err != nil {
					t.Fatal(err)
				}
			}

			roleBindingStore := kubeInformerFactory.Rbac().V1().RoleBindings().Informer().GetStore()
			for _, roleBinding := range c.roleBindings {
				if err := roleBindingStore.Add(roleBinding); err != nil {
					t.Fatal(err)
				}
			}

			clusterClient := fakeclusterclient.NewSimpleClientset()
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}
			workClient := fakeworkclient.NewSimpleClientset()
			workInformerFactory := workinformers.NewSharedInformerFactory(workClient, 5*time.Minute)
			workStore := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
			for _, work := range c.works {
				if err := workStore.Add(work); err != nil {
					t.Fatal(err)
				}
			}

			_ = NewFinalizeController(
				kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				kubeInformerFactory.Core().V1().Namespaces(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				workInformerFactory.Work().V1().ManifestWorks().Lister(),
				kubeClient.RbacV1(),
				events.NewInMemoryRecorder(""),
			)

			ctrl := &finalizeController{
				roleBindingLister:  kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				namespaceLister:    kubeInformerFactory.Core().V1().Namespaces().Lister(),
				clusterLister:      clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister(),
				rbacClient:         kubeClient.RbacV1(),
				eventRecorder:      events.NewInMemoryRecorder(""),
			}
			err := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, c.key))
			testingcommon.AssertError(t, err, c.expectedErr)
		})
	}
}

func TestSyncRoleAndRoleBinding(t *testing.T) {
	cases := []struct {
		name                          string
		roleBinding                   *rbacv1.RoleBinding
		namespace                     string
		expectedRoleFinalizers        []string
		expectedRoleBindingFinalizers []string
		expectedWorkFinalizers        []string
		validateRbacActions           func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                "skip if rolebinding does not exists",
			namespace:           testinghelpers.TestManagedClusterName,
			validateRbacActions: testingcommon.AssertNoActions,
		},
		{
			name: "skip if rolebinding has no finalizer", roleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, nil, nil, false),
			namespace:           testinghelpers.TestManagedClusterName,
			validateRbacActions: testingcommon.AssertNoActions,
		},
		{
			name:                          "skip if rolebinding has no labels",
			roleBinding:                   testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, nil, false),
			namespace:                     testinghelpers.TestManagedClusterName,
			expectedRoleBindingFinalizers: []string{manifestWorkFinalizer},
			validateRbacActions:           testingcommon.AssertNoActions,
		},
		{
			name: "remove finalizer from deleting roleBinding",
			roleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer},
				map[string]string{clusterv1.ClusterNameLabelKey: testinghelpers.TestManagedClusterName}, true),
			namespace:              testinghelpers.TestManagedClusterName,
			expectedRoleFinalizers: []string{manifestWorkFinalizer},
			validateRbacActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.roleBinding != nil {
				objects = append(objects, c.roleBinding)
			}
			fakeClient := fakeclient.NewSimpleClientset(objects...)

			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeClient, time.Minute*10)

			recorder := events.NewInMemoryRecorder("")
			controller := finalizeController{
				roleBindingLister: kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				eventRecorder:     recorder,
				rbacClient:        fakeClient.RbacV1(),
			}

			controllerContext := testingcommon.NewFakeSyncContext(t, "")

			func() {
				ctx, cancel := context.WithTimeout(context.TODO(), 15*time.Second)
				defer cancel()

				kubeInformerFactory.Start(ctx.Done())
				kubeInformerFactory.WaitForCacheSync(ctx.Done())
				fakeClient.ClearActions()
				if err := controller.syncRoleBindings(context.TODO(), controllerContext, c.namespace); err != nil {
					t.Fatal(err)
				}

				c.validateRbacActions(t, fakeClient.Actions())

				if c.roleBinding != nil {
					rolebinding, err := fakeClient.RbacV1().RoleBindings(c.roleBinding.Namespace).Get(context.TODO(), c.roleBinding.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					testinghelpers.AssertFinalizers(t, rolebinding, c.expectedRoleBindingFinalizers)
				}

			}()
		})
	}
}
