package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/helpers"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// SpokeClusterAdmissionHook will validate the creating/updating spokeclusters request.
type SpokeClusterAdmissionHook struct {
	kubeClient kubernetes.Interface
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
// webhook is accessed by the kube apiserver.
func (a *SpokeClusterAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	return schema.GroupVersionResource{
			Group:    "admission.cluster.open-cluster-management.io",
			Version:  "v1",
			Resource: "spokeclustervalidators",
		},
		"spokeclustervalidator"
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
func (a *SpokeClusterAdmissionHook) Validate(admissionSpec *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Infof("validate %q operation for object %q", admissionSpec.Operation, admissionSpec.Object)

	status := &admissionv1beta1.AdmissionResponse{}

	// only validate the request for spokeclusters
	if admissionSpec.Resource.Group != "cluster.open-cluster-management.io" ||
		admissionSpec.Resource.Version != "v1" ||
		admissionSpec.Resource.Resource != "spokeclusters" {
		status.Allowed = true
		return status
	}

	switch admissionSpec.Operation {
	case admissionv1beta1.Create:
		return a.validateCreateRequest(admissionSpec)
	case admissionv1beta1.Update:
		return a.validateUpdateRequest(admissionSpec)
	default:
		status.Allowed = true
		return status
	}
}

// Initialize is called by generic-admission-server on startup to setup initialization that spokeclusters webhook needs.
func (a *SpokeClusterAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	var err error
	a.kubeClient, err = kubernetes.NewForConfig(kubeClientConfig)
	return err
}

// validateCreateRequest validates create spoke cluster operation
func (a *SpokeClusterAdmissionHook) validateCreateRequest(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

	// validate SpokeCluster object firstly
	spokeCluster, err := a.validateSpokeClusterObj(request.Object)
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	// the HubAcceptsClient field is not changed, finish the validation process
	if !spokeCluster.Spec.HubAcceptsClient {
		status.Allowed = true
		return status
	}

	// the HubAcceptsClient field is changed, we need to check the request user whether
	// has been allowed to change the HubAcceptsClient field with SubjectAccessReview api
	return a.allowUpdateAcceptField(request.UserInfo)
}

// validateUpdateRequest validates update spoke cluster operation.
func (a *SpokeClusterAdmissionHook) validateUpdateRequest(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

	oldSpokeCluster := &clusterv1.SpokeCluster{}
	if err := json.Unmarshal(request.OldObject.Raw, oldSpokeCluster); err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	// validate the updating SpokeCluster object firstly
	newSpokeCluster, err := a.validateSpokeClusterObj(request.Object)
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	// the HubAcceptsClient field is not changed, finish the validation process
	if newSpokeCluster.Spec.HubAcceptsClient == oldSpokeCluster.Spec.HubAcceptsClient {
		status.Allowed = true
		return status
	}

	// the HubAcceptsClient field is changed, we need to check the request user whether
	// has been allowed to update the HubAcceptsClient field with SubjectAccessReview api
	return a.allowUpdateAcceptField(request.UserInfo)
}

// validateSpokeClusterObj validates the fileds of SpokeCluster object
func (a *SpokeClusterAdmissionHook) validateSpokeClusterObj(requestObj runtime.RawExtension) (*clusterv1.SpokeCluster, error) {
	errs := []error{}

	spokeCluster := &clusterv1.SpokeCluster{}
	if err := json.Unmarshal(requestObj.Raw, spokeCluster); err != nil {
		errs = append(errs, err)
	}

	// there are no spoke client configs, finish the validation process
	if len(spokeCluster.Spec.SpokeClientConfigs) == 0 {
		return spokeCluster, operatorhelpers.NewMultiLineAggregate(errs)
	}

	// validate the url in spoke client configs
	for _, clientConfig := range spokeCluster.Spec.SpokeClientConfigs {
		if !helpers.IsValidHTTPSURL(clientConfig.URL) {
			errs = append(errs, fmt.Errorf("url %q is invalid in spoke client configs", clientConfig.URL))
		}
	}

	return spokeCluster, operatorhelpers.NewMultiLineAggregate(errs)
}

// allowUpdateHubAcceptsClientField using SubjectAccessReview API to check whether a request user has been authorized to update
// HubAcceptsClient field
func (a *SpokeClusterAdmissionHook) allowUpdateAcceptField(userInfo authenticationv1.UserInfo) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

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
				Group:       "register.open-cluster-management.io",
				Resource:    "spokeclusters",
				Verb:        "update",
				Subresource: "acceptance",
			},
		},
	}
	sar, err := a.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusForbidden, Reason: metav1.StatusReasonForbidden,
			Message: err.Error(),
		}
		return status
	}

	if !sar.Status.Allowed {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusForbidden, Reason: metav1.StatusReasonForbidden,
			Message: fmt.Sprintf("user %q cannot update the HubAcceptsClient field", userInfo.Username),
		}
		return status
	}

	status.Allowed = true
	return status
}
