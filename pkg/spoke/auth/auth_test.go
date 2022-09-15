package auth_test

import (
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/auth"
)

func TestValidate(t *testing.T) {

	tests := map[string]struct {
		executor  *workapiv1.ManifestWorkExecutor
		namespace string
		name      string
		action    auth.ExecuteAction
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
		"action invalid": {
			executor: &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
						Namespace: "test-ns",
						Name:      "test-name",
					},
				},
			},
			action: "test-action",
			expect: fmt.Errorf("execute action test-action is invalid"),
		},
		"forbideen": {
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
			action:    auth.ApplyAction,
			expect:    fmt.Errorf("not allowed to apply the resource  secrets, name: test, will try again in 1m0s"),
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
			action:    auth.ApplyAction,
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
	validator := auth.NewExecutorValidator(kubeClient)
	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			err := validator.Validate(context.TODO(), test.executor, gvr, test.namespace, test.name, test.action)
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
