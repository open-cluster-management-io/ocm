package webhook

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/open-cluster-management/work/pkg/spoke/spoketesting"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var manifestWorkSchema = metav1.GroupVersionResource{
	Group:    "work.open-cluster-management.io",
	Version:  "v1",
	Resource: "manifestworks",
}

func TestManifestWorkValidate(t *testing.T) {
	manifestLimit = 3
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
				spoketesting.NewUnstructured("v1", "Kind", "testns", "test"),
				spoketesting.NewUnstructured("v1", "Kind", "testns", "test1"),
				spoketesting.NewUnstructured("v1", "Kind", "testns", "test2"),
				spoketesting.NewUnstructured("v1", "Kind", "testns", "test3"),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "number of manifests should not be larger than 3",
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
