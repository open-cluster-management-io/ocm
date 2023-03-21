package basic

import (
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

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
	validator := NewSARValidator(nil, kubeClient)
	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			err := validator.Validate(context.TODO(), test.executor, gvr, test.namespace, test.name, true, nil)
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

func TestValidateEscalation(t *testing.T) {

	tests := map[string]struct {
		executor  *workapiv1.ManifestWorkExecutor
		namespace string
		name      string
		obj       *unstructured.Unstructured
		expect    error
	}{
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
			obj:       spoketesting.NewUnstructured("v1", "ClusterRole", "", "test"),
			expect:    fmt.Errorf("not allowed to apply the resource rbac.authorization.k8s.io roles, test-deny test, error: permission escalation, will try again in 1m0s"),
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
			obj:       spoketesting.NewUnstructured("v1", "Role", "ns1", "test"),
			expect:    nil,
		},
	}

	gvr := schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}
	kubeClient := fakekube.NewSimpleClientset()
	kubeClient.PrependReactor("create", "subjectaccessreviews",
		func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, &v1.SubjectAccessReview{
				Status: v1.SubjectAccessReviewStatus{
					Allowed: true,
				},
			}, nil
		},
	)

	scheme := runtime.NewScheme()
	dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme)
	dynamicClient.PrependReactor("create", "roles",
		func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			obj := action.(clienttesting.CreateActionImpl).Object.(*unstructured.Unstructured)

			if obj.GetKind() == "Role" {
				return true, obj, nil
			}
			if obj.GetKind() == "ClusterRole" {
				return true, obj, apierrors.NewForbidden(
					schema.GroupResource{Group: "rbac.authorization.k8s.io", Resource: "clusterroles"},
					obj.GetName(),
					fmt.Errorf("escalation"))
			}
			return false, nil, nil
		})
	validator := &SarValidator{
		kubeClient: kubeClient,
		newImpersonateClientFunc: func(config *rest.Config, username string) (dynamic.Interface, error) {
			return dynamicClient, nil
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			err := validator.Validate(context.TODO(), test.executor, gvr, test.namespace, test.name, true, test.obj)
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
