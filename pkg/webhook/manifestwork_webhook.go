package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"

	workv1 "open-cluster-management.io/api/work/v1"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// ManifestLimit is the max size of manifests data which is 50k bytes.
const ManifestLimit = 50 * 1024

// ManifestWorkAdmissionHook will validate the creating/updating manifestwork request.
type ManifestWorkAdmissionHook struct{}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
// webhook is accessed by the kube apiserver.
func (a *ManifestWorkAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	return schema.GroupVersionResource{
			Group:    "admission.work.open-cluster-management.io",
			Version:  "v1",
			Resource: "manifestworkvalidators",
		},
		"manifestworkvalidators"
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
func (a *ManifestWorkAdmissionHook) Validate(admissionSpec *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Infof("validate %q operation for object %q", admissionSpec.Operation, admissionSpec.Object)

	status := &admissionv1beta1.AdmissionResponse{}

	// only validate the request for manifestwork
	if admissionSpec.Resource.Group != "work.open-cluster-management.io" ||
		admissionSpec.Resource.Resource != "manifestworks" {
		status.Allowed = true
		return status
	}

	switch admissionSpec.Operation {
	case admissionv1beta1.Create:
		return a.validateRequest(admissionSpec)
	case admissionv1beta1.Update:
		return a.validateRequest(admissionSpec)
	default:
		status.Allowed = true
		return status
	}
}

// Initialize is called by generic-admission-server on startup to setup initialization that manifestwork webhook needs.
func (a *ManifestWorkAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	return nil
}

// validateRequest validates creating/updating manifestwork operation
func (a *ManifestWorkAdmissionHook) validateRequest(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

	err := a.validateManifestWorkObj(request.Object)
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	status.Allowed = true
	return status
}

// validateManifestWorkObj validates the fileds of manifestwork object
func (a *ManifestWorkAdmissionHook) validateManifestWorkObj(requestObj runtime.RawExtension) error {
	work := &workv1.ManifestWork{}
	if err := json.Unmarshal(requestObj.Raw, work); err != nil {
		return err
	}

	if len(work.Spec.Workload.Manifests) == 0 {
		return fmt.Errorf("manifests should not be empty")
	}

	totalSize := 0
	for _, manifest := range work.Spec.Workload.Manifests {
		totalSize = totalSize + manifest.Size()
	}

	if totalSize > ManifestLimit {
		return fmt.Errorf("the size of manifests is %v bytes which exceeds the 50k limit", totalSize)
	}

	for _, manifest := range work.Spec.Workload.Manifests {
		err := a.validateManifest(manifest.Raw)
		if err != nil {
			return err
		}
	}

	return nil
}

func (a *ManifestWorkAdmissionHook) validateManifest(manifest []byte) error {
	// If the manifest cannot be decoded, return err
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(manifest)
	if err != nil {
		return err
	}

	// The object must have name specified, generateName is not allowed in manifestwork
	if unstructuredObj.GetName() == "" {
		return fmt.Errorf("name must be set in manifest")
	}

	if unstructuredObj.GetGenerateName() != "" {
		return fmt.Errorf("generateName must not be set in manifest")
	}

	return nil
}
