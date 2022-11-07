package v1

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	_ "open-cluster-management.io/work/pkg/features"

	admissionv1 "k8s.io/api/admission/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fakekube "k8s.io/client-go/kubernetes/fake"

	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

var manifestWorkSchema = metav1.GroupVersionResource{
	Group:    "work.open-cluster-management.io",
	Version:  "v1",
	Resource: "manifestworks",
}

func TestValidateCreateUpdate(t *testing.T) {
	w := ManifestWorkWebhook{}
	err := w.ValidateCreate(context.Background(), &workv1.ManifestWorkList{})
	if err == nil {
		t.Errorf("Non work obj, Expect Error but got nil")
	}
	err = w.ValidateUpdate(context.Background(), nil, &workv1.ManifestWork{})
	if err == nil {
		t.Errorf("Non work obj, Expect Error but got nil")
	}
	err = w.ValidateUpdate(context.Background(), &workv1.ManifestWork{}, nil)
	if err == nil {
		t.Errorf("Non work obj, Expect Error but got nil")
	}
}

func TestManifestWorkExecutorValidate(t *testing.T) {
	cases := []struct {
		name        string
		request     admission.Request
		manifests   []*unstructured.Unstructured
		oldExecutor *workv1.ManifestWorkExecutor
		executor    *workv1.ManifestWorkExecutor
		expectErr   error
	}{
		{
			name: "validate executor nil success",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  manifestWorkSchema,
					Operation: admissionv1.Create,
					UserInfo:  authenticationv1.UserInfo{Username: "test1"},
				},
			},
			manifests: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "kind",
						"metadata": map[string]interface{}{
							"namespace": "ns1",
							"name":      "test",
						},
					},
				},
			},
			expectErr: nil,
		},
		{
			name: "validate executor nil fail",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  manifestWorkSchema,
					Operation: admissionv1.Create,
					UserInfo:  authenticationv1.UserInfo{Username: "test2"},
				},
			},
			manifests: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "kind",
						"metadata": map[string]interface{}{
							"namespace": "ns1",
							"name":      "test",
						},
					},
				},
			},
			expectErr: apierrors.NewBadRequest(fmt.Sprintf("user test2 cannot manipulate the Manifestwork with executor /klusterlet-work-sa in namespace cluster1")),
		},
		{
			name: "validate executor not nil success",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  manifestWorkSchema,
					Operation: admissionv1.Create,
					UserInfo:  authenticationv1.UserInfo{Username: "test1"},
				},
			},
			manifests: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "kind",
						"metadata": map[string]interface{}{
							"namespace": "ns1",
							"name":      "test",
						},
					},
				},
			},
			executor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor1",
					},
				},
			},
			expectErr: nil,
		},
		{
			name: "validate executor not nil fail",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  manifestWorkSchema,
					Operation: admissionv1.Create,
					UserInfo:  authenticationv1.UserInfo{Username: "test1"},
				},
			},
			manifests: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "kind",
						"metadata": map[string]interface{}{
							"namespace": "ns1",
							"name":      "test",
						},
					},
				},
			},
			executor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor2",
					},
				},
			},
			expectErr: apierrors.NewBadRequest(fmt.Sprintf("user test1 cannot manipulate the Manifestwork with executor ns1/executor2 in namespace cluster1")),
		},
		{
			name: "validate executor not changed success",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  manifestWorkSchema,
					Operation: admissionv1.Update,
					UserInfo:  authenticationv1.UserInfo{Username: "test1"},
				},
			},
			manifests: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "kind",
						"metadata": map[string]interface{}{
							"namespace": "ns1",
							"name":      "test",
						},
					},
				},
			},
			executor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor2",
					},
				},
			},
			oldExecutor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor2",
					},
				},
			},
			expectErr: nil,
		},
		{
			name: "validate executor changed fail",
			request: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource:  manifestWorkSchema,
					Operation: admissionv1.Update,
					UserInfo:  authenticationv1.UserInfo{Username: "test1"},
				},
			},
			manifests: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "kind",
						"metadata": map[string]interface{}{
							"namespace": "ns1",
							"name":      "test",
						},
					},
				},
			},
			executor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor2",
					},
				},
			},
			oldExecutor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor1",
					},
				},
			},
			expectErr: apierrors.NewBadRequest(fmt.Sprintf("user test1 cannot manipulate the Manifestwork with executor ns1/executor2 in namespace cluster1")),
		},
	}

	utilruntime.Must(utilfeature.DefaultMutableFeatureGate.Set(
		fmt.Sprintf("%s=true", ocmfeature.NilExecutorValidating),
	))

	kubeClient := fakekube.NewSimpleClientset()
	kubeClient.PrependReactor("create", "subjectaccessreviews",
		func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			obj := action.(clienttesting.CreateActionImpl).Object.(*v1.SubjectAccessReview)

			if obj.Spec.User == "test1" &&
				reflect.DeepEqual(obj.Spec.ResourceAttributes, &v1.ResourceAttributes{
					Group:     "work.open-cluster-management.io",
					Resource:  "manifestworks",
					Verb:      "execute-as",
					Namespace: "cluster1",
					Name:      "system:serviceaccount::klusterlet-work-sa",
				}) {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Allowed: true,
					},
				}, nil
			}

			if obj.Spec.User == "test1" &&
				reflect.DeepEqual(obj.Spec.ResourceAttributes, &v1.ResourceAttributes{
					Group:     "work.open-cluster-management.io",
					Resource:  "manifestworks",
					Verb:      "execute-as",
					Namespace: "cluster1",
					Name:      "system:serviceaccount:ns1:executor1",
				}) {
				return true, &v1.SubjectAccessReview{
					Status: v1.SubjectAccessReviewStatus{
						Allowed: true,
					},
				}, nil
			}

			return true, &v1.SubjectAccessReview{
				Status: v1.SubjectAccessReviewStatus{
					Allowed: false,
				},
			}, nil

		},
	)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mw := ManifestWorkWebhook{
				kubeClient: kubeClient,
			}
			ctx := admission.NewContextWithRequest(context.Background(), c.request)
			newWork, _ := spoketesting.NewManifestWork(0, c.manifests...)
			var oldWork *workv1.ManifestWork
			if c.request.Operation == "UPDATE" {
				oldWork = newWork.DeepCopy()
				oldWork.Spec.Executor = c.oldExecutor
			}
			newWork.Spec.Executor = c.executor
			err := mw.validateRequest(newWork, oldWork, ctx)
			if !reflect.DeepEqual(err, c.expectErr) {
				t.Errorf("case: %v, expected %v but got: %v", c.name, c.expectErr, err)
			}
		})
	}
}
