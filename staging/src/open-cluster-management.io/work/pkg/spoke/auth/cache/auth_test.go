package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	v1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	k8scache "k8s.io/client-go/tools/cache"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/auth/basic"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func newExecutorCacheValidator(t *testing.T, ctx context.Context, clusterName string,
	kubeClient kubernetes.Interface, manifestWorkObjects ...runtime.Object) *sarCacheValidator {

	workClient := fakeworkclient.NewSimpleClientset(manifestWorkObjects...)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(
		workClient, 5*time.Minute, workinformers.WithNamespace(clusterName))

	basicValidater := basic.NewSARValidator(nil, kubeClient)
	validator := NewExecutorCacheValidator(ctx, eventstesting.NewTestingEventRecorder(t), kubeClient,
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(clusterName),
		spoketesting.NewFakeRestMapper(),
		basicValidater,
	)

	go func() {
		// Wait for cache synced before starting to make sure all manifestworks could be processed
		k8scache.WaitForNamedCacheSync("ExecutorCacheValidator", ctx.Done(),
			workInformerFactory.Work().V1().ManifestWorks().Informer().HasSynced)
		validator.Start(ctx)
	}()
	return validator
}

func TestValidate(t *testing.T) {

	tests := map[string]struct {
		executor  *workapiv1.ManifestWorkExecutor
		namespace string
		name      string
		expect    error
	}{
		"executor nil": {
			executor: nil,
			expect:   nil,
		},
		"unsupported type": {
			executor: &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: "test",
				},
			},
			expect: fmt.Errorf("only support %s type for the executor", workapiv1.ExecutorSubjectTypeServiceAccount),
		},
		"sa nil": {
			executor: &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
				},
			},
			expect: fmt.Errorf("the executor service account is nil"),
		},
		"forbidden": {
			executor: &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
						Namespace: "test-ns",
						Name:      "test-name",
					},
				},
			},
			namespace: "test-deny",
			name:      "test",
			expect:    fmt.Errorf("not allowed to apply the resource  secrets, test-deny test, will try again in 1m0s"),
		},
		"allow": {
			executor: &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
						Namespace: "test-ns",
						Name:      "test-name",
					},
				},
			},
			namespace: "test-allow",
			name:      "test",
			expect:    nil,
		},
	}

	gvr := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	kubeClient := fakekube.NewSimpleClientset()
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
	cacheValidator := newExecutorCacheValidator(t, ctx, clusterName, kubeClient)
	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			err := cacheValidator.Validate(context.TODO(), test.executor, gvr, test.namespace, test.name, true, nil)
			if test.expect == nil {
				if err != nil {
					t.Errorf("expect nil but got %s", err)
				}
			} else if err == nil || err.Error() != test.expect.Error() {
				t.Errorf("expect %s but got %s", test.expect, err)
			}
		})
	}
}

func TestCacheWorks(t *testing.T) {
	executor := &workapiv1.ManifestWorkExecutor{
		Subject: workapiv1.ManifestWorkExecutorSubject{
			Type: workapiv1.ExecutorSubjectTypeServiceAccount,
			ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
				Namespace: "test-ns",
				Name:      "test-name",
			},
		},
	}

	tests := map[string]struct {
		executor  *workapiv1.ManifestWorkExecutor
		namespace string
		name      string
		expect    error
	}{
		"forbidden": {
			executor:  executor,
			namespace: "test-deny",
			name:      "test",
			expect:    fmt.Errorf("not allowed to apply the resource  secrets, test-deny test, will try again in 1m0s"),
		},
		"allow": {
			executor:  executor,
			namespace: "test-allow",
			name:      "test",
			expect:    nil,
		},
	}

	gvr := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	kubeClient := fakekube.NewSimpleClientset()
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

	cacheValidator := newExecutorCacheValidator(t, ctx, clusterName, kubeClient, work)
	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			// call validate 10 times
			for i := 0; i < 10; i++ {
				err := cacheValidator.Validate(context.TODO(), test.executor, gvr, test.namespace, test.name, true, nil)
				if test.expect == nil {
					if err != nil {
						t.Errorf("expect nil but got %s", err)
					}
				} else if err == nil || err.Error() != test.expect.Error() {
					t.Errorf("expect %s but got %s", test.expect, err)
				}
			}
		})
	}

	actualSARActions := []clienttesting.Action{}
	for _, action := range kubeClient.Actions() {
		if action.GetResource().Resource == "subjectaccessreviews" {
			actualSARActions = append(actualSARActions, action)
		}
	}

	// the expected 6 comes from:
	//   * 5(allowed sar check for Get, List, Update, Patch, Delete)
	//   * 1(denied sar check for Get; after the first Get check fails, subsequent checks do not need to be checked)
	if len(actualSARActions) != 6 {
		t.Errorf("Expected kube client has 6 subject access review action but got %#v", len(actualSARActions))
	}
}
