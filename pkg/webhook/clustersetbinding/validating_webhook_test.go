package clustersetbinding

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

var managedclustersetbindingSchema = metav1.GroupVersionResource{
	Group:    "cluster.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "managedclustersetbindings",
}

func TestManagedClusterValidate(t *testing.T) {
	cases := []struct {
		name                     string
		request                  *admissionv1beta1.AdmissionRequest
		expectedResponse         *admissionv1beta1.AdmissionResponse
		allowBindingToClusterSet bool
	}{
		{
			name: "validate non-managedclustersetbindings request",
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
				Resource:  managedclustersetbindingSchema,
				Operation: admissionv1beta1.Delete,
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating cluster set binding",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersetbindingSchema,
				Operation: admissionv1beta1.Create,
				Object:    newManagedClusterSetBindingObj("ns1", "cs1", "cs1", nil),
			},
			allowBindingToClusterSet: true,
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating cluster set binding with unmatched name",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersetbindingSchema,
				Operation: admissionv1beta1.Create,
				Object:    newManagedClusterSetBindingObj("ns1", "csb1", "cs1", nil),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "The ManagedClusterSetBinding must have the same name as the target ManagedClusterSet",
				},
			},
		},
		{
			name: "validate creating cluster set binding without permission",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersetbindingSchema,
				Operation: admissionv1beta1.Create,
				Object:    newManagedClusterSetBindingObj("ns1", "cs1", "cs1", nil),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusForbidden, Reason: metav1.StatusReasonForbidden,
					Message: "user \"\" is not allowed to bind cluster set \"cs1\"",
				},
			},
		},
		{
			name: "validate updating cluster set binding",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersetbindingSchema,
				Operation: admissionv1beta1.Update,
				Object:    newManagedClusterSetBindingObj("ns1", "cs1", "cs1", nil),
				OldObject: newManagedClusterSetBindingObj("ns1", "cs1", "cs2", map[string]string{
					"team": "team1",
				}),
			},
			allowBindingToClusterSet: true,
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate updating cluster set binding with different cluster set",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersetbindingSchema,
				Operation: admissionv1beta1.Update,
				Object:    newManagedClusterSetBindingObj("ns1", "cs1", "cs2", nil),
				OldObject: newManagedClusterSetBindingObj("ns1", "cs1", "cs1", nil),
			},
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "The ManagedClusterSetBinding must have the same name as the target ManagedClusterSet",
				},
			},
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
							Allowed: c.allowBindingToClusterSet,
						},
					}, nil
				},
			)

			admissionHook := &ManagedClusterSetBindingValidatingAdmissionHook{
				kubeClient: kubeClient,
			}

			actualResponse := admissionHook.Validate(c.request)
			if !reflect.DeepEqual(actualResponse, c.expectedResponse) {
				t.Errorf("expected %#v but got: %#v", c.expectedResponse.Result, actualResponse.Result)
			}
		})
	}
}

func newManagedClusterSetBindingObj(namespace, name, clusterSetName string, labels map[string]string) runtime.RawExtension {
	managedClusterSetBinding := &clusterv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
		Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: clusterSetName,
		},
	}
	bindingObj, _ := json.Marshal(managedClusterSetBinding)
	return runtime.RawExtension{
		Raw: bindingObj,
	}
}
