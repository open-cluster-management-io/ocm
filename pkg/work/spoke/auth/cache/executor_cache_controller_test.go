package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	k8scache "k8s.io/client-go/tools/cache"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth/basic"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth/store"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
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
	controllerFactory := newControllerInner(cacheController,
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

			if obj.Spec.ResourceAttributes.Namespace == allowNS {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Allowed: true,
					},
				}, nil
			}

			if obj.Spec.ResourceAttributes.Namespace == denyNS {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Denied: true,
					},
				}, nil
			}
			return false, nil, nil
		},
	)

	ctx := context.TODO()

	work, _ := spoketesting.NewManifestWork(0,
		testingcommon.NewUnstructured("v1", "Secret", allowNS, "test"),
		testingcommon.NewUnstructured("v1", "Secret", denyNS, "test"),
	)
	work.Spec.Executor = executor
	work.Spec.DeleteOption = &workapiv1.DeleteOption{
		PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
		SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
			OrphaningRules: []workapiv1.OrphaningRule{
				{
					Group:     "",
					Resource:  "secrets",
					Namespace: allowNS,
					Name:      "test",
				},
			},
		},
	}

	initialized := make(chan struct{})
	cacheController := newExecutorCacheController(t, ctx, clusterName, kubeClient, initialized, work)
	<-initialized

	// the RBAC resources that already exist when the informers start all enqueue the same
	// executor key, and the queue only collapses the keys added before the worker dequeues
	// them, so the initial state may be synced more than once. Wait for that to settle and
	// take the result as the baseline of the deletions below.
	baseline := waitForInitialSync(t, kubeClient, cacheController.bindingExecutorsMapper, 2)

	// check if the map is initialized
	executorKey := fmt.Sprintf("%s/%s", executor.Subject.ServiceAccount.Namespace, executor.Subject.ServiceAccount.Name)
	checkBindingExecutorMapperInitialized(t, cacheController.bindingExecutorsMapper,
		fmt.Sprintf("%s/%s", roleNamespace, roleName), executorKey)
	checkBindingExecutorMapperInitialized(t, cacheController.bindingExecutorsMapper,
		roleName, executorKey)

	err := kubeClient.RbacV1().ClusterRoles().Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, baseline+1*sarChecksPerSync)
	if err != nil {
		t.Error(err)
	}

	err = kubeClient.RbacV1().Roles(roleNamespace).Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, baseline+2*sarChecksPerSync)
	if err != nil {
		t.Error(err)
	}

	err = kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, baseline+3*sarChecksPerSync)
	if err != nil {
		t.Error(err)
	}

	err = kubeClient.RbacV1().RoleBindings(roleNamespace).Delete(ctx, roleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Exepected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, baseline+4*sarChecksPerSync)
	if err != nil {
		t.Error(err)
	}
}

const (
	pollInterval = 50 * time.Millisecond
	pollTimeout  = 30 * time.Second
	// settledPollCount is how many consecutive polls must observe the same state before the
	// initial sync is considered done
	settledPollCount = 10
)

// sarChecksPerSync is the number of subject access review requests a single executor sync sends:
//   - 4(allowed sar check for Get, List, Update, Patch)
//   - 1(denied sar check for Get; after the first Get check fails, subsequent checks do not need to be checked)
const sarChecksPerSync = 5

// waitForInitialSync waits until the controller has reconciled the RBAC resources that exist
// before the informers start: the binding executor mapper holds the expected bindings and the
// subject access review requests stop coming in. It returns the number of requests sent so far,
// which is the baseline the following assertions build on.
func waitForInitialSync(t *testing.T, kubeClient *fakekube.Clientset, mapper *safeMap, expectedBindings int) int {
	t.Helper()

	var count, lastCount, settledPolls int
	err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, pollTimeout, true,
		func(context.Context) (bool, error) {
			count = countSARRequests(kubeClient.Actions())
			if count > 0 && count == lastCount && mapper.count() == expectedBindings {
				settledPolls++
			} else {
				settledPolls = 0
			}
			lastCount = count
			return settledPolls >= settledPollCount, nil
		})
	if err != nil {
		t.Fatalf("Initial sync did not settle, got %d subject access review actions and %d binding executor mapper items (expected %d items)",
			count, mapper.count(), expectedBindings)
	}

	if count%sarChecksPerSync != 0 {
		t.Fatalf("Expected a multiple of %d subject access review actions after the initial sync but got %d",
			sarChecksPerSync, count)
	}

	t.Logf("The initial sync sent %d subject access review requests", count)
	return count
}

func checkSARCount(kubeClient *fakekube.Clientset, expected int) error {
	var actual int
	err := wait.PollUntilContextTimeout(context.TODO(), pollInterval, pollTimeout, true,
		func(context.Context) (bool, error) {
			actual = countSARRequests(kubeClient.Actions())
			if actual > expected {
				// the count only grows, so it will never match again
				return false, fmt.Errorf("Expected kube client has %d subject access review action but got %d",
					expected, actual)
			}
			return actual == expected, nil
		})
	if err != nil {
		return fmt.Errorf("Expected kube client has %d subject access review action but got %d", expected, actual)
	}

	return nil
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

// TestCacheControllerClusterRoleWithRoleBindingOnly verifies that when a ClusterRole changes,
// the controller finds RoleBindings referencing it via the byClusterRole index (not byRole).
func TestCacheControllerClusterRoleWithRoleBindingOnly(t *testing.T) {
	executor := &workapiv1.ManifestWorkExecutor{
		Subject: workapiv1.ManifestWorkExecutorSubject{
			Type: workapiv1.ExecutorSubjectTypeServiceAccount,
			ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
				Namespace: "test-ns",
				Name:      "test-name",
			},
		},
	}

	clusterRoleName := "test-cluster-role"
	rbNamespace := "test-ns"

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
			},
		},
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rb-for-cluster-role",
			Namespace: rbNamespace,
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
			Name:     clusterRoleName,
		},
	}

	kubeClient := fakekube.NewSimpleClientset(clusterRole, roleBinding)
	kubeClient.PrependReactor("create", "subjectaccessreviews",
		func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			obj := action.(clienttesting.CreateActionImpl).Object.(*v1.SubjectAccessReview)

			if obj.Spec.ResourceAttributes.Namespace == allowNS {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Allowed: true,
					},
				}, nil
			}

			if obj.Spec.ResourceAttributes.Namespace == denyNS {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Denied: true,
					},
				}, nil
			}
			return false, nil, nil
		},
	)

	ctx := context.TODO()

	work, _ := spoketesting.NewManifestWork(0,
		testingcommon.NewUnstructured("v1", "Secret", allowNS, "test"),
		testingcommon.NewUnstructured("v1", "Secret", denyNS, "test"),
	)
	work.Spec.Executor = executor
	work.Spec.DeleteOption = &workapiv1.DeleteOption{
		PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
		SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
			OrphaningRules: []workapiv1.OrphaningRule{
				{
					Group:     "",
					Resource:  "secrets",
					Namespace: allowNS,
					Name:      "test",
				},
			},
		},
	}

	initialized := make(chan struct{})
	cacheController := newExecutorCacheController(t, ctx, clusterName, kubeClient, initialized, work)
	<-initialized

	baseline := waitForInitialSync(t, kubeClient, cacheController.bindingExecutorsMapper, 1)

	executorKey := fmt.Sprintf("%s/%s",
		executor.Subject.ServiceAccount.Namespace, executor.Subject.ServiceAccount.Name)
	checkBindingExecutorMapperInitialized(t, cacheController.bindingExecutorsMapper,
		fmt.Sprintf("%s/%s", rbNamespace, "rb-for-cluster-role"), executorKey)

	err := kubeClient.RbacV1().ClusterRoles().Delete(ctx, clusterRoleName, metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}

	err = checkSARCount(kubeClient, baseline+sarChecksPerSync)
	if err != nil {
		t.Errorf("ClusterRole deletion did not trigger cache refresh through RoleBinding path: %v", err)
	}
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
