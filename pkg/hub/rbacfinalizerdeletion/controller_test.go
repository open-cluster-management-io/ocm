package rbacfinalizerdeletion

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/diff"
)

func newRole(namespace, name string, finalizers []string, terminated bool) *rbacv1.Role {
	role := &rbacv1.Role{}

	role.Namespace = namespace
	role.Name = name
	role.Finalizers = finalizers
	if terminated {
		now := metav1.Now()
		role.DeletionTimestamp = &now
	}

	return role
}

func newRoleBinding(namespace, name string, finalizers []string, terminated bool) *rbacv1.RoleBinding {
	rolebinding := &rbacv1.RoleBinding{}

	rolebinding.Namespace = namespace
	rolebinding.Name = name
	rolebinding.Finalizers = finalizers
	if terminated {
		now := metav1.Now()
		rolebinding.DeletionTimestamp = &now
	}

	return rolebinding
}

func newNamespace(name string, terminated bool) *corev1.Namespace {
	namespace := &corev1.Namespace{}
	namespace.Name = name
	if terminated {
		now := metav1.Now()
		namespace.DeletionTimestamp = &now
	}

	return namespace
}

func newManifestWork(namespace, name string, finalizers []string, deletionTimestamp *metav1.Time) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			Finalizers:        finalizers,
			DeletionTimestamp: deletionTimestamp,
		},
	}

	return work
}

func newDeletionTimestamp(offset time.Duration) *metav1.Time {
	return &metav1.Time{
		Time: metav1.Now().Add(offset),
	}
}

func TestSyncRoleAndRoleBinding(t *testing.T) {
	now := metav1.Now()
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
			cluster:             testinghelpers.NewManagedCluster("cluster1", nil),
			namespace:           newNamespace("cluster1", false),
			work:                testinghelpers.NewManifestWork("cluster1", "work1", nil, nil),
			validateRbacActions: noAction,
		},
		{
			name:                   "skip if neither role nor rolebinding has finalizer",
			role:                   newRole("cluster1", "cluster1:spoke-work", nil, false),
			roleBinding:            newRoleBinding("cluster1", "cluster1:spoke-work", nil, false),
			cluster:                testinghelpers.NewManagedCluster("cluster1", nil),
			namespace:              newNamespace("cluster1", false),
			work:                   testinghelpers.NewManifestWork("cluster1", "work1", []string{manifestWorkFinalizer}, nil),
			expectedWorkFinalizers: []string{manifestWorkFinalizer},
			validateRbacActions:    noAction,
		},
		{
			name:                          "remove finalizer from deleting role within non-terminating namespace",
			role:                          newRole("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			roleBinding:                   newRoleBinding("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, false),
			cluster:                       testinghelpers.NewManagedCluster("cluster1", nil),
			namespace:                     newNamespace("cluster1", false),
			work:                          testinghelpers.NewManifestWork("cluster1", "work1", []string{manifestWorkFinalizer}, nil),
			expectedRoleBindingFinalizers: []string{manifestWorkFinalizer},
			expectedWorkFinalizers:        []string{manifestWorkFinalizer},
			validateRbacActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:        "remove finalizer from role/rolebinding within terminating cluster",
			role:        newRole("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			roleBinding: newRoleBinding("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			cluster:     testinghelpers.NewManagedCluster("cluster1", &now),
			namespace:   newNamespace("cluster1", false),
			validateRbacActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:        "remove finalizer from role/rolebinding within terminating ns",
			role:        newRole("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			roleBinding: newRoleBinding("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			namespace:   newNamespace("cluster1", true),
			validateRbacActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}
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
					assertFinalizers(t, role, c.expectedRoleFinalizers)
				}

				if c.roleBinding != nil {
					rolebinding, err := fakeClient.RbacV1().RoleBindings(c.roleBinding.Namespace).Get(context.TODO(), c.roleBinding.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assertFinalizers(t, rolebinding, c.expectedRoleBindingFinalizers)
				}

				if c.work != nil {
					work, err := fakeManifestWorkClient.WorkV1().ManifestWorks(c.work.Namespace).Get(context.TODO(), c.work.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assertFinalizers(t, work, c.expectedWorkFinalizers)
				}

				actual := controllerContext.Queue().Len()
				if actual != c.expectedQueueLen {
					t.Errorf("Expect queue with length: %d, but got %d", c.expectedQueueLen, actual)
				}
			}()
		})
	}
}

func assertFinalizers(t *testing.T, obj runtime.Object, finalizers []string) {
	accessor, _ := meta.Accessor(obj)
	actual := accessor.GetFinalizers()
	if len(actual) == 0 && len(finalizers) == 0 {
		return
	}

	if !reflect.DeepEqual(actual, finalizers) {
		t.Fatal(diff.ObjectDiff(actual, finalizers))
	}
}

func noAction(t *testing.T, actions []clienttesting.Action) {
	if len(actions) > 0 {
		t.Fatal(spew.Sdump(actions))
	}
}
