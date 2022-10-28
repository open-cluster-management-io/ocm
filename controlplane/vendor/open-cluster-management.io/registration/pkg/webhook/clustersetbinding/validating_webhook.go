package clustersetbinding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

// ManagedClusterSetBindingValidatingAdmissionHook will validate the creating/updating ManagedClusterSetBinding request.
type ManagedClusterSetBindingValidatingAdmissionHook struct {
	kubeClient kubernetes.Interface
}

func (h *ManagedClusterSetBindingValidatingAdmissionHook) SetKubeClient(c kubernetes.Interface) {
	h.kubeClient = c
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
// webhook is accessed by the kube apiserver.
func (a *ManagedClusterSetBindingValidatingAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	return schema.GroupVersionResource{
			Group:    "admission.cluster.open-cluster-management.io",
			Version:  "v1",
			Resource: "managedclustersetbindingvalidators",
		},
		"managedclustersetbindingvalidators"
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
func (a *ManagedClusterSetBindingValidatingAdmissionHook) Validate(admissionSpec *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Infof("validate %q operation for object %q", admissionSpec.Operation, admissionSpec.Object)

	// only validate the request for ManagedClusterSetBinding
	if admissionSpec.Resource.Group != "cluster.open-cluster-management.io" ||
		admissionSpec.Resource.Resource != "managedclustersetbindings" {
		return acceptRequest()
	}

	// only handle Create/Update Operation
	if admissionSpec.Operation != admissionv1beta1.Create && admissionSpec.Operation != admissionv1beta1.Update {
		return acceptRequest()
	}

	binding := &clusterv1beta1.ManagedClusterSetBinding{}
	if err := json.Unmarshal(admissionSpec.Object.Raw, binding); err != nil {
		return denyRequest(http.StatusBadRequest, metav1.StatusReasonBadRequest,
			fmt.Sprintf("Unable to unmarshal the ManagedClusterSetBinding object: %v", err))
	}

	// force the instance name to match the target cluster set name
	if binding.Name != binding.Spec.ClusterSet {
		return denyRequest(http.StatusBadRequest, metav1.StatusReasonBadRequest,
			"The ManagedClusterSetBinding must have the same name as the target ManagedClusterSet")
	}

	// check if the request user has permission to bind the target cluster set
	if admissionSpec.Operation == admissionv1beta1.Create {
		return a.allowBindingToClusterSet(binding.Spec.ClusterSet, admissionSpec.UserInfo)
	}

	return acceptRequest()
}

// Initialize is called by generic-admission-server on startup to setup initialization that ManagedClusterSetBinding webhook needs.
func (a *ManagedClusterSetBindingValidatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	var err error
	a.kubeClient, err = kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	return nil
}

// allowBindingToClusterSet checks if the user has permission to bind a particular cluster set
func (a *ManagedClusterSetBindingValidatingAdmissionHook) allowBindingToClusterSet(clusterSetName string, userInfo authenticationv1.UserInfo) *admissionv1beta1.AdmissionResponse {
	extra := make(map[string]authorizationv1.ExtraValue)
	for k, v := range userInfo.Extra {
		extra[k] = authorizationv1.ExtraValue(v)
	}

	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   userInfo.Username,
			UID:    userInfo.UID,
			Groups: userInfo.Groups,
			Extra:  extra,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:       "cluster.open-cluster-management.io",
				Resource:    "managedclustersets",
				Subresource: "bind",
				Verb:        "create",
				Name:        clusterSetName,
			},
		},
	}
	sar, err := a.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		return denyRequest(http.StatusForbidden, metav1.StatusReasonForbidden, err.Error())
	}
	if !sar.Status.Allowed {
		return denyRequest(http.StatusForbidden, metav1.StatusReasonForbidden, fmt.Sprintf("user %q is not allowed to bind cluster set %q", userInfo.Username, clusterSetName))
	}
	return acceptRequest()
}

func acceptRequest() *admissionv1beta1.AdmissionResponse {
	return &admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}
}

func denyRequest(code int32, reason metav1.StatusReason, message string) *admissionv1beta1.AdmissionResponse {
	return &admissionv1beta1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    code,
			Reason:  reason,
			Message: message,
		},
	}
}
