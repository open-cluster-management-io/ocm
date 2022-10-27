package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

// ManagedClusterValidatingAdmissionHook will validate the creating/updating managedcluster request.
type ManagedClusterValidatingAdmissionHook struct {
	kubeClient kubernetes.Interface
}

func (h *ManagedClusterValidatingAdmissionHook) SetKubeClient(c kubernetes.Interface) {
	h.kubeClient = c
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
// webhook is accessed by the kube apiserver.
func (a *ManagedClusterValidatingAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	return schema.GroupVersionResource{
			Group:    "admission.cluster.open-cluster-management.io",
			Version:  "v1",
			Resource: "managedclustervalidators",
		},
		"managedclustervalidators"
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
func (a *ManagedClusterValidatingAdmissionHook) Validate(admissionSpec *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Infof("validate %q operation for object %q", admissionSpec.Operation, admissionSpec.Object)

	status := &admissionv1beta1.AdmissionResponse{}

	// only validate the request for managedcluster
	if admissionSpec.Resource.Group != "cluster.open-cluster-management.io" ||
		admissionSpec.Resource.Resource != "managedclusters" {
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

// Initialize is called by generic-admission-server on startup to setup initialization that managedclusters webhook needs.
func (a *ManagedClusterValidatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	var err error
	a.kubeClient, err = kubernetes.NewForConfig(kubeClientConfig)
	return err
}

// validateCreateRequest validates create managed cluster operation
func (a *ManagedClusterValidatingAdmissionHook) validateCreateRequest(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

	// validate ManagedCluster object firstly
	managedCluster, err := a.validateManagedClusterObj(request.Object)
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	if managedCluster.Spec.HubAcceptsClient {
		// the HubAcceptsClient field is changed, we need to check the request user whether
		// has been allowed to change the HubAcceptsClient field with SubjectAccessReview api
		if status := a.allowUpdateAcceptField(managedCluster.Name, request.UserInfo); !status.Allowed {
			return status
		}
	}

	// check whether the request user has been allowed to set clusterset label
	var clusterSetName string
	if len(managedCluster.Labels) > 0 {
		clusterSetName = managedCluster.Labels[clusterv1beta1.ClusterSetLabel]
	}

	return a.allowSetClusterSetLabel(request.UserInfo, "", clusterSetName)
}

// validateUpdateRequest validates update managed cluster operation.
func (a *ManagedClusterValidatingAdmissionHook) validateUpdateRequest(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

	oldManagedCluster := &clusterv1.ManagedCluster{}
	if err := json.Unmarshal(request.OldObject.Raw, oldManagedCluster); err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	// validate the updating ManagedCluster object firstly
	newManagedCluster, err := a.validateManagedClusterObj(request.Object)
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	if newManagedCluster.Spec.HubAcceptsClient != oldManagedCluster.Spec.HubAcceptsClient {
		// the HubAcceptsClient field is changed, we need to check the request user whether
		// has been allowed to update the HubAcceptsClient field with SubjectAccessReview api
		if status := a.allowUpdateAcceptField(newManagedCluster.Name, request.UserInfo); !status.Allowed {
			return status
		}
	}

	// check whether the request user has been allowed to set clusterset label
	var originalClusterSetName, currentClusterSetName string
	if len(oldManagedCluster.Labels) > 0 {
		originalClusterSetName = oldManagedCluster.Labels[clusterv1beta1.ClusterSetLabel]
	}
	if len(newManagedCluster.Labels) > 0 {
		currentClusterSetName = newManagedCluster.Labels[clusterv1beta1.ClusterSetLabel]
	}

	return a.allowSetClusterSetLabel(request.UserInfo, originalClusterSetName, currentClusterSetName)
}

// validateManagedClusterObj validates the fileds of ManagedCluster object
func (a *ManagedClusterValidatingAdmissionHook) validateManagedClusterObj(requestObj runtime.RawExtension) (*clusterv1.ManagedCluster, error) {
	errs := []error{}

	managedCluster := &clusterv1.ManagedCluster{}
	if err := json.Unmarshal(requestObj.Raw, managedCluster); err != nil {
		errs = append(errs, err)
	}

	// there are no spoke client configs, finish the validation process
	if len(managedCluster.Spec.ManagedClusterClientConfigs) == 0 {
		return managedCluster, operatorhelpers.NewMultiLineAggregate(errs)
	}

	// validate the url in spoke client configs
	for _, clientConfig := range managedCluster.Spec.ManagedClusterClientConfigs {
		if !helpers.IsValidHTTPSURL(clientConfig.URL) {
			errs = append(errs, fmt.Errorf("url %q is invalid in client configs", clientConfig.URL))
		}
	}

	return managedCluster, operatorhelpers.NewMultiLineAggregate(errs)
}

// allowUpdateHubAcceptsClientField using SubjectAccessReview API to check whether a request user has been authorized to update
// HubAcceptsClient field
func (a *ManagedClusterValidatingAdmissionHook) allowUpdateAcceptField(clusterName string, userInfo authenticationv1.UserInfo) *admissionv1beta1.AdmissionResponse {
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
				Resource:    "managedclusters",
				Verb:        "update",
				Subresource: "accept",
				Name:        clusterName,
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

// allowSetClusterSetLabel checks whether a request user has been authorized to set clusterset label
func (a *ManagedClusterValidatingAdmissionHook) allowSetClusterSetLabel(userInfo authenticationv1.UserInfo, originalClusterSet, newClusterSet string) *admissionv1beta1.AdmissionResponse {
	if originalClusterSet == newClusterSet {
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	if len(originalClusterSet) > 0 {
		if status := a.allowUpdateClusterSet(userInfo, originalClusterSet); !status.Allowed {
			return status
		}
	}

	if len(newClusterSet) > 0 {
		if status := a.allowUpdateClusterSet(userInfo, newClusterSet); !status.Allowed {
			return status
		}
	}

	return &admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}
}

// allowUpdateClusterSet checks whether a request user has been authorized to add/remove a ManagedCluster
// to/from the ManagedClusterSet
func (a *ManagedClusterValidatingAdmissionHook) allowUpdateClusterSet(userInfo authenticationv1.UserInfo, clusterSetName string) *admissionv1beta1.AdmissionResponse {
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
				Group:       "cluster.open-cluster-management.io",
				Resource:    "managedclustersets",
				Subresource: "join",
				Name:        clusterSetName,
				Verb:        "create",
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
			Message: fmt.Sprintf("user %q cannot add/remove a ManagedCluster to/from ManagedClusterSet %q", userInfo.Username, clusterSetName),
		}
		return status
	}

	status.Allowed = true
	return status
}
