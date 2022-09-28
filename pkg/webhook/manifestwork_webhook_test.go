package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"
	_ "open-cluster-management.io/work/pkg/features"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

var manifestWorkSchema = metav1.GroupVersionResource{
	Group:    "work.open-cluster-management.io",
	Version:  "v1",
	Resource: "manifestworks",
}

func TestManifestWorkValidate(t *testing.T) {
	cases := []struct {
		name             string
		request          *admissionv1beta1.AdmissionRequest
		manifests        []*unstructured.Unstructured
		expectedResponse *admissionv1beta1.AdmissionResponse
	}{
		{
			name: "validate non-manifestwork request",
			request: &admissionv1beta1.AdmissionRequest{
				Resource: metav1.GroupVersionResource{
					Group:    "test.open-cluster-management.io",
					Version:  "v1",
					Resource: "tests",
				},
			},
			manifests: []*unstructured.Unstructured{},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate deleting operation",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Delete,
			},
			manifests: []*unstructured.Unstructured{},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating ManifestWork",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
			},
			manifests: []*unstructured.Unstructured{spoketesting.NewUnstructured("v1", "Kind", "testns", "test")},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating ManifestWork with no manifests",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
			},
			manifests: []*unstructured.Unstructured{},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "manifests should not be empty",
				},
			},
		},
		{
			name: "validate creating ManifestWork with no name",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
				UserInfo:  authenticationv1.UserInfo{Username: "tester"},
			},
			manifests: []*unstructured.Unstructured{
				spoketesting.NewUnstructured("v1", "Kind", "testns", "test"),
				spoketesting.NewUnstructured("v1", "Kind", "testns", ""),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "name must be set in manifest",
				},
			},
		},
		{
			name: "validate creating ManifestWork with generate name",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
				UserInfo:  authenticationv1.UserInfo{Username: "tester"},
			},
			manifests: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "kind",
						"metadata": map[string]interface{}{
							"namespace":    "ns1",
							"name":         "test",
							"generateName": "test",
						},
					},
				},
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "generateName must not be set in manifest",
				},
			},
		},
		{
			name: "validate updating ManifestWork with no name",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Update,
				UserInfo:  authenticationv1.UserInfo{Username: "tester"},
			},
			manifests: []*unstructured.Unstructured{
				spoketesting.NewUnstructured("v1", "Kind", "testns", "test"),
				spoketesting.NewUnstructured("v1", "Kind", "testns", ""),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "name must be set in manifest",
				},
			},
		},
		{
			name: "validate creating ManifestWork with manifests more than limit",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
				UserInfo:  authenticationv1.UserInfo{Username: "tester"},
			},
			manifests: []*unstructured.Unstructured{
				spoketesting.NewUnstructuredSecretBySize("test1", "testns", 10*1024),
				spoketesting.NewUnstructuredSecretBySize("test2", "testns", 10*1024),
				spoketesting.NewUnstructuredSecretBySize("test3", "testns", 10*1024),
				spoketesting.NewUnstructuredSecretBySize("test4", "testns", 10*1024),
				spoketesting.NewUnstructuredSecretBySize("test5", "testns", 10*1024),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "the size of manifests is 51685 bytes which exceeds the 50k limit",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, _ := spoketesting.NewManifestWork(0, c.manifests...)
			c.request.Object.Raw, _ = json.Marshal(work)
			admissionHook := &ManifestWorkAdmissionHook{}
			actualResponse := admissionHook.Validate(c.request)
			if !reflect.DeepEqual(actualResponse, c.expectedResponse) {
				t.Errorf("expected %#v but got: %#v", c.expectedResponse.Result, actualResponse.Result)
			}
		})
	}
}

func TestManifestWorkExecutorValidate(t *testing.T) {
	cases := []struct {
		name             string
		request          *admissionv1beta1.AdmissionRequest
		manifests        []*unstructured.Unstructured
		oldExecutor      *workv1.ManifestWorkExecutor
		executor         *workv1.ManifestWorkExecutor
		expectedResponse *admissionv1beta1.AdmissionResponse
	}{
		{
			name: "validate executor nil success",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
				UserInfo:  authenticationv1.UserInfo{Username: "test1"},
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
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate executor nil fail",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
				UserInfo:  authenticationv1.UserInfo{Username: "test2"},
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
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "user test2 cannot manipulate the Manifestwork with executor /klusterlet-work-sa in namespace cluster1",
				},
			},
		},
		{
			name: "validate executor not nil success",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
				UserInfo:  authenticationv1.UserInfo{Username: "test1"},
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
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate executor not nil fail",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Create,
				UserInfo:  authenticationv1.UserInfo{Username: "test1"},
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
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "user test1 cannot manipulate the Manifestwork with executor ns1/executor2 in namespace cluster1",
				},
			},
		},
		{
			name: "validate executor not changed success",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Update,
				UserInfo:  authenticationv1.UserInfo{Username: "test1"},
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
			oldExecutor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor2",
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
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate executor changed fail",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  manifestWorkSchema,
				Operation: admissionv1beta1.Update,
				UserInfo:  authenticationv1.UserInfo{Username: "test1"},
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
			oldExecutor: &workv1.ManifestWorkExecutor{
				Subject: workv1.ManifestWorkExecutorSubject{
					Type: workv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workv1.ManifestWorkSubjectServiceAccount{
						Namespace: "ns1",
						Name:      "executor1",
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
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "user test1 cannot manipulate the Manifestwork with executor ns1/executor2 in namespace cluster1",
				},
			},
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
	admissionHook := &ManifestWorkAdmissionHook{
		kubeClient: kubeClient,
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, _ := spoketesting.NewManifestWork(0, c.manifests...)
			work.Spec.Executor = c.executor
			c.request.Object.Raw, _ = json.Marshal(work)
			if c.request.Operation == admissionv1beta1.Update {
				oldWork := work.DeepCopy()
				oldWork.Spec.Executor = c.oldExecutor
				c.request.OldObject.Raw, _ = json.Marshal(oldWork)
			}

			actualResponse := admissionHook.Validate(c.request)
			if !reflect.DeepEqual(actualResponse, c.expectedResponse) {
				t.Errorf("expected %#v but got: %#v", c.expectedResponse.Result, actualResponse.Result)
			}
		})
	}
}
