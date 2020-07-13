package rbacfinalizerdeletion

import (
	"context"
	"fmt"
	"testing"
	"time"

	fakeclusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	"github.com/openshift/library-go/pkg/operator/events"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

var roleName = fmt.Sprintf("%s:spoke-work", testinghelpers.TestManagedClusterName)

func TestSync(t *testing.T) {
	cases := []struct {
		name         string
		key          string
		clusters     []runtime.Object
		namespaces   []runtime.Object
		roles        []runtime.Object
		roleBindings []runtime.Object
		works        []runtime.Object
		expectedErr  string
	}{
		{
			name:        "managed cluster namespace is not found",
			key:         fmt.Sprintf("%s/%s", testinghelpers.TestManagedClusterName, roleName),
			expectedErr: "namespace \"testmanagedcluster\" not found",
		},
		{
			name:       "there are no resources in managed cluster namespace",
			key:        fmt.Sprintf("%s/%s", testinghelpers.TestManagedClusterName, roleName),
			namespaces: []runtime.Object{testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, true)},
		},
		{
			name:         "still have works in deleting managed cluster namespace",
			key:          fmt.Sprintf("%s/%s", testinghelpers.TestManagedClusterName, roleName),
			namespaces:   []runtime.Object{testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, true)},
			roles:        []runtime.Object{testinghelpers.NewRole(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, true)},
			roleBindings: []runtime.Object{testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, true)},
			works:        []runtime.Object{testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName, "work1", []string{manifestWorkFinalizer}, nil)},
			expectedErr:  "Still having 1 works in the cluster namespace testmanagedcluster",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakeclient.NewSimpleClientset()
			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)
			nsStore := kubeInformerFactory.Core().V1().Namespaces().Informer().GetStore()
			for _, ns := range c.namespaces {
				nsStore.Add(ns)
			}
			roleStore := kubeInformerFactory.Rbac().V1().Roles().Informer().GetStore()
			for _, role := range c.roles {
				roleStore.Add(role)
			}
			roleBindingStore := kubeInformerFactory.Rbac().V1().RoleBindings().Informer().GetStore()
			for _, roleBinding := range c.roleBindings {
				roleBindingStore.Add(roleBinding)
			}

			clusterClient := fakeclusterclient.NewSimpleClientset()
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)

			workClient := fakeworkclient.NewSimpleClientset()
			workInformerFactory := workinformers.NewSharedInformerFactory(workClient, 5*time.Minute)
			workStore := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
			for _, work := range c.works {
				workStore.Add(work)
			}

			ctrl := &finalizeController{
				roleLister:         kubeInformerFactory.Rbac().V1().Roles().Lister(),
				roleBindingLister:  kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				namespaceLister:    kubeInformerFactory.Core().V1().Namespaces().Lister(),
				clusterLister:      clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister(),
				rbacClient:         kubeClient.RbacV1(),
				eventRecorder:      events.NewInMemoryRecorder(""),
			}
			err := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, c.key))
			testinghelpers.AssertError(t, err, c.expectedErr)
		})
	}
}

func TestSyncRoleAndRoleBinding(t *testing.T) {
	cases := []struct {
		name                          string
		role                          *rbacv1.Role
		roleBinding                   *rbacv1.RoleBinding
		cluster                       *clusterv1.ManagedCluster
		namespace                     *corev1.Namespace
		work                          *workapiv1.ManifestWork
		expectedRoleFinalizers        []string
		expectedRoleBindingFinalizers []string
		expectedWorkFinalizers        []string
		expectedQueueLen              int
		validateRbacActions           func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                "skip if neither role nor rolebinding exists",
			cluster:             testinghelpers.NewManagedCluster(),
			namespace:           testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, false),
			work:                testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName, "work1", nil, nil),
			validateRbacActions: testinghelpers.AssertNoActions,
		},
		{
			name:                   "skip if neither role nor rolebinding has finalizer",
			role:                   testinghelpers.NewRole(testinghelpers.TestManagedClusterName, roleName, nil, false),
			roleBinding:            testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, nil, false),
			cluster:                testinghelpers.NewManagedCluster(),
			namespace:              testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, false),
			work:                   testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName, "work1", []string{manifestWorkFinalizer}, nil),
			expectedWorkFinalizers: []string{manifestWorkFinalizer},
			validateRbacActions:    testinghelpers.AssertNoActions,
		},
		{
			name:                          "remove finalizer from deleting role within non-terminating namespace",
			role:                          testinghelpers.NewRole(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, true),
			roleBinding:                   testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, false),
			cluster:                       testinghelpers.NewManagedCluster(),
			namespace:                     testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, false),
			work:                          testinghelpers.NewManifestWork(testinghelpers.TestManagedClusterName, "work1", []string{manifestWorkFinalizer}, nil),
			expectedRoleBindingFinalizers: []string{manifestWorkFinalizer},
			expectedWorkFinalizers:        []string{manifestWorkFinalizer},
			validateRbacActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
			},
		},
		{
			name:        "remove finalizer from role/rolebinding within terminating cluster",
			role:        testinghelpers.NewRole(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, true),
			roleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, true),
			cluster:     testinghelpers.NewDeletingManagedCluster(),
			namespace:   testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, false),
			validateRbacActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update", "update")
			},
		},
		{
			name:        "remove finalizer from role/rolebinding within terminating ns",
			role:        testinghelpers.NewRole(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, true),
			roleBinding: testinghelpers.NewRoleBinding(testinghelpers.TestManagedClusterName, roleName, []string{manifestWorkFinalizer}, true),
			namespace:   testinghelpers.NewNamespace(testinghelpers.TestManagedClusterName, true),
			validateRbacActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update", "update")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.role != nil {
				objects = append(objects, c.role)
			}
			if c.roleBinding != nil {
				objects = append(objects, c.roleBinding)
			}
			fakeClient := fakeclient.NewSimpleClientset(objects...)

			var fakeManifestWorkClient *fakeworkclient.Clientset
			if c.work == nil {
				fakeManifestWorkClient = fakeworkclient.NewSimpleClientset()
			} else {
				fakeManifestWorkClient = fakeworkclient.NewSimpleClientset(c.work)
			}

			workInformerFactory := workinformers.NewSharedInformerFactory(fakeManifestWorkClient, 5*time.Minute)

			recorder := events.NewInMemoryRecorder("")
			controller := finalizeController{
				manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister(),
				eventRecorder:      recorder,
				rbacClient:         fakeClient.RbacV1(),
			}

			controllerContext := testinghelpers.NewFakeSyncContext(t, "")

			func() {
				ctx, cancel := context.WithTimeout(context.TODO(), 15*time.Second)
				defer cancel()

				workInformerFactory.Start(ctx.Done())
				workInformerFactory.WaitForCacheSync(ctx.Done())

				controller.syncRoleAndRoleBinding(context.TODO(), controllerContext, c.role, c.roleBinding, c.namespace, c.cluster)

				c.validateRbacActions(t, fakeClient.Actions())

				if c.role != nil {
					role, err := fakeClient.RbacV1().Roles(c.role.Namespace).Get(context.TODO(), c.role.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					testinghelpers.AssertFinalizers(t, role, c.expectedRoleFinalizers)
				}

				if c.roleBinding != nil {
					rolebinding, err := fakeClient.RbacV1().RoleBindings(c.roleBinding.Namespace).Get(context.TODO(), c.roleBinding.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					testinghelpers.AssertFinalizers(t, rolebinding, c.expectedRoleBindingFinalizers)
				}

				if c.work != nil {
					work, err := fakeManifestWorkClient.WorkV1().ManifestWorks(c.work.Namespace).Get(context.TODO(), c.work.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					testinghelpers.AssertFinalizers(t, work, c.expectedWorkFinalizers)
				}

				actual := controllerContext.Queue().Len()
				if actual != c.expectedQueueLen {
					t.Errorf("Expect queue with length: %d, but got %d", c.expectedQueueLen, actual)
				}
			}()
		})
	}
}
