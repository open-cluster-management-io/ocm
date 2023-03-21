package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	v1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	k8scache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/auth/basic"
	"open-cluster-management.io/work/pkg/spoke/auth/store"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func newExecutorCacheController(t *testing.T, ctx context.Context, clusterName string,
	kubeClient kubernetes.Interface, initialized chan struct{}, manifestWorkObjects ...runtime.Object) *CacheController {

	workClient := fakeworkclient.NewSimpleClientset(manifestWorkObjects...)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(
		workClient, 5*time.Minute, workinformers.WithNamespace(clusterName))
	manifestWorkLister := workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName)
	manifestWorkExecutorCachesLoader := &defaultManifestWorkExecutorCachesLoader{
		manifestWorkLister: manifestWorkLister,
		restMapper:         spoketesting.NewFakeRestMapper(),
	}

	spokeInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 1*time.Hour)

	cacheController := &CacheController{
		executorCaches:                   store.NewExecutorCache(),
		manifestWorkExecutorCachesLoader: manifestWorkExecutorCachesLoader,
		sarCheckerFn:                     basic.NewSARValidator(nil, kubeClient).CheckSubjectAccessReviews,
		bindingExecutorsMapper:           newSafeMap(),
	}
	controllerFactory := newControllerInner(cacheController, eventstesting.NewTestingEventRecorder(t),
		spokeInformer.Rbac().V1().ClusterRoleBindings(),
		spokeInformer.Rbac().V1().RoleBindings(),
		spokeInformer.Rbac().V1().ClusterRoles(),
		spokeInformer.Rbac().V1().Roles(),
	)

	go func() {
		workInformerFactory.Start(ctx.Done())
		// Wait for cache synced before starting to make sure all manifestworks could be processed
		k8scache.WaitForNamedCacheSync("ExecutorCacheValidator", ctx.Done(),
			workInformerFactory.Work().V1().ManifestWorks().Informer().HasSynced)

		// initialize the caches skelton in order to let others caches operands know which caches are necessary,
		// otherwise, the roleBindingExecutorsMapper and clusterRoleBindingExecutorsMapper in the cache controller
		// have no chance to initialize after the work pod restarts
		cacheController.manifestWorkExecutorCachesLoader.loadAllValuableCaches(cacheController.executorCaches)

		spokeInformer.Start(ctx.Done())
		spokeInformer.WaitForCacheSync(ctx.Done())
		initialized <- struct{}{}
		controllerFactory.Run(ctx, 1)
	}()

	return cacheController
}

func TestCacheController(t *testing.T) {
	executor := &workapiv1.ManifestWorkExecutor{
		Subject: workapiv1.ManifestWorkExecutorSubject{
			Type: workapiv1.ExecutorSubjectTypeServiceAccount,
			ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
				Namespace: "test-ns",
				Name:      "test-name",
			},
		},
	}

	roleName := "cluster-role-1"
	roleNamespace := "test-ns"
	role1 := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: roleNamespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
			},
		},
	}

	roleBinding1 := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: roleNamespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: executor.Subject.ServiceAccount.Namespace,
				Name:      executor.Subject.ServiceAccount.Name,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     roleName,
		},
	}

	clusterRole1 := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: roleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
			},
		},
	}

	clusterRoleBinding1 := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: executor.Subject.ServiceAccount.Namespace,
				Name:      executor.Subject.ServiceAccount.Name,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     roleName,
		},
	}

	kubeClient := fakekube.NewSimpleClientset(role1, roleBinding1, clusterRole1, clusterRoleBinding1)
	kubeClient.PrependReactor("create", "subjectaccessreviews",
		func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			obj := action.(clienttesting.CreateActionImpl).Object.(*v1.SubjectAccessReview)

			if obj.Spec.ResourceAttributes.Namespace == "test-allow" {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Allowed: true,
					},
				}, nil
			}

			if obj.Spec.ResourceAttributes.Namespace == "test-deny" {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Denied: true,
					},
				}, nil
			}
			return false, nil, nil
		},
	)

	clusterName := "cluster1"
	ctx := context.TODO()

	work, _ := spoketesting.NewManifestWork(0,
		spoketesting.NewUnstructured("v1", "Secret", "test-allow", "test"),
		spoketesting.NewUnstructured("v1", "Secret", "test-deny", "test"),
	)
	work.Spec.Executor = executor
	work.Spec.DeleteOption = &workapiv1.DeleteOption{
		PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
		SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
			OrphaningRules: []workapiv1.OrphaningRule{
				{
					Group:     "",
					Resource:  "secrets",
					Namespace: "test-allow",
					Name:      "test",
				},
			},
		},
	}

	initialized := make(chan struct{})
	cacheController := newExecutorCacheController(t, ctx, clusterName, kubeClient, initialized, work)
	<-initialized

	// the expected 5 comes from:
	//   * 4(allowed sar check for Get, List, Update, Patch)
	//   * 1(denied sar check for Get; after the first Get check fails, subsequent checks do not need to be checked)
	err := checkSARCount(kubeClient, 5)
	if err != nil {
		t.Error(err)
	}

	// check if the map is initialized
	executorKey := fmt.Sprintf("%s/%s", executor.Subject.ServiceAccount.Namespace, executor.Subject.ServiceAccount.Name)
	actualMapCount := cacheController.bindingExecutorsMapper.count()
	if actualMapCount != 2 {
		t.Errorf("Expected 2 map item but got %d", actualMapCount)
	}
	checkBindingExecutorMapperInitialized(t, cacheController.bindingExecutorsMapper,
		fmt.Sprintf("%s/%s", roleNamespace, roleName), executorKey)
	checkBindingExecutorMapperInitialized(t, cacheController.bindingExecutorsMapper,
		roleName, executorKey)

	err = kubeClient.RbacV1().ClusterRoles().Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, 2*5)
	if err != nil {
		t.Error(err)
	}

	err = kubeClient.RbacV1().Roles(roleNamespace).Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, 3*5)
	if err != nil {
		t.Error(err)
	}

	err = kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, 4*5)
	if err != nil {
		t.Error(err)
	}

	err = kubeClient.RbacV1().RoleBindings(roleNamespace).Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, 5*5)
	if err != nil {
		t.Error(err)
	}
}

func checkSARCount(kubeClient *fakekube.Clientset, expected int) error {
	return retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			return err != nil
		},
		func() error {
			acture := countSARRequests(kubeClient.Actions())
			if acture != expected {
				return fmt.Errorf("Expected kube client has %d subject access review action but got %#v",
					expected, acture)
			}
			return nil
		})

}

func countSARRequests(kubeClientActions []clienttesting.Action) int {
	actualSARActions := []clienttesting.Action{}
	for _, action := range kubeClientActions {
		if action.GetResource().Resource == "subjectaccessreviews" {
			actualSARActions = append(actualSARActions, action)
		}
	}

	return len(actualSARActions)
}

func checkBindingExecutorMapperInitialized(t *testing.T, m *safeMap, roleKey, executorKey string) {
	actualExecutors := m.get(roleKey)
	if len(actualExecutors) != 1 {
		t.Errorf("Expected role key %s has 1 executor but got %d", roleKey, len(actualExecutors))
	}
	if executorKey != actualExecutors[0] {
		t.Errorf("Expected role key %s has the executor %s but got %s", roleKey, executorKey, actualExecutors[0])
	}
}
