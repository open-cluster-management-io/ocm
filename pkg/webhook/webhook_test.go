package webhook

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

var spokeclustersSchema = metav1.GroupVersionResource{
	Group:    "cluster.open-cluster-management.io",
	Version:  "v1",
	Resource: "spokeclusters",
}

func TestSpokeClusterValidate(t *testing.T) {
	cases := []struct {
		name                   string
		request                *admissionv1beta1.AdmissionRequest
		expectedResponse       *admissionv1beta1.AdmissionResponse
		allowUpdateAcceptField bool
	}{
		{
			name: "validate non-spokeclusters request",
			request: &admissionv1beta1.AdmissionRequest{
				Resource: metav1.GroupVersionResource{
					Group:    "test.open-cluster-management.io",
					Version:  "v1",
					Resource: "tests",
				},
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate deleting operation",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Delete,
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating SpokeCluster",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Create,
				Object:    newSpokeClusterObj(),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating SpokeCluster with invalid fields",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Create,
				Object:    newSpokeClusterObjWithClientConfigs(clusterv1.ClientConfig{URL: "http://127.0.0.1:8001"}),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "url \"http://127.0.0.1:8001\" is invalid in spoke client configs",
				},
			},
		},
		{
			name: "validate creating an accepted SpokeCluster without update acceptance permission",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Create,
				Object:    newSpokeClusterObjWithHubAcceptsClient(true),
				UserInfo:  authenticationv1.UserInfo{Username: "tester"},
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusForbidden, Reason: metav1.StatusReasonForbidden,
					Message: "user \"tester\" cannot update the HubAcceptsClient field",
				},
			},
		},
		{
			name: "validate creating an accepted SpokeCluster",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Create,
				Object:    newSpokeClusterObjWithHubAcceptsClient(true),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
			allowUpdateAcceptField: true,
		},
		{
			name: "validate update SpokeCluster without HubAcceptsClient field changed",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Update,
				OldObject: newSpokeClusterObjWithClientConfigs(clusterv1.ClientConfig{URL: "https://127.0.0.1:6443"}),
				Object:    newSpokeClusterObjWithClientConfigs(clusterv1.ClientConfig{URL: "https://127.0.0.1:8443"}),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate updating HubAcceptsClient field without update acceptance permission",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Update,
				OldObject: newSpokeClusterObjWithHubAcceptsClient(false),
				Object:    newSpokeClusterObjWithHubAcceptsClient(true),
				UserInfo:  authenticationv1.UserInfo{Username: "tester"},
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusForbidden, Reason: metav1.StatusReasonForbidden,
					Message: "user \"tester\" cannot update the HubAcceptsClient field",
				},
			},
		},
		{
			name: "validate updating HubAcceptsClient field",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  spokeclustersSchema,
				Operation: admissionv1beta1.Update,
				OldObject: newSpokeClusterObjWithHubAcceptsClient(false),
				Object:    newSpokeClusterObjWithHubAcceptsClient(true),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
			allowUpdateAcceptField: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset()
			kubeClient.PrependReactor(
				"create",
				"subjectaccessreviews",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, &authorizationv1.SubjectAccessReview{
						Status: authorizationv1.SubjectAccessReviewStatus{
							Allowed: c.allowUpdateAcceptField,
						},
					}, nil
				},
			)

			admissionHook := &SpokeClusterAdmissionHook{kubeClient: kubeClient}

			actualResponse := admissionHook.Validate(c.request)

			if !reflect.DeepEqual(actualResponse, c.expectedResponse) {
				t.Errorf("expected %#v but got: %#v", c.expectedResponse.Result, actualResponse.Result)
			}
		})
	}
}

func newSpokeClusterObj() runtime.RawExtension {
	spokeCluster := &clusterv1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testspokecluster",
		},
	}
	clusterObj, _ := json.Marshal(spokeCluster)
	return runtime.RawExtension{
		Raw: clusterObj,
	}
}

func newSpokeClusterObjWithHubAcceptsClient(hubAcceptsClient bool) runtime.RawExtension {
	spokeCluster := &clusterv1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testspokecluster",
		},
		Spec: clusterv1.SpokeClusterSpec{
			HubAcceptsClient: hubAcceptsClient,
		},
	}
	clusterObj, _ := json.Marshal(spokeCluster)
	return runtime.RawExtension{
		Raw: clusterObj,
	}
}

func newSpokeClusterObjWithClientConfigs(clientConfig clusterv1.ClientConfig) runtime.RawExtension {
	spokeCluster := &clusterv1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testspokecluster",
		},
		Spec: clusterv1.SpokeClusterSpec{
			SpokeClientConfigs: []clusterv1.ClientConfig{clientConfig},
		},
	}
	clusterObj, _ := json.Marshal(spokeCluster)
	return runtime.RawExtension{
		Raw: clusterObj,
	}
}
