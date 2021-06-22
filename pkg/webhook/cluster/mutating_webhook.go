package cluster

import (
	"encoding/json"
	"net/http"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const defaultLeaseDurationSecondsPatch = `[{"op": "replace", "path": "/spec/leaseDurationSeconds", "value": 60}]`

// ManagedClusterMutatingAdmissionHook will mutate the creating/updating managedcluster request.
type ManagedClusterMutatingAdmissionHook struct{}

// MutatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
// webhook is accessed by the kube apiserver.
func (a *ManagedClusterMutatingAdmissionHook) MutatingResource() (schema.GroupVersionResource, string) {
	return schema.GroupVersionResource{
			Group:    "admission.cluster.open-cluster-management.io",
			Version:  "v1",
			Resource: "managedclustermutators",
		},
		"managedclustermutators"
}

// Admit is called by generic-admission-server when the registered REST resource above is called with an admission request.
func (a *ManagedClusterMutatingAdmissionHook) Admit(req *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Infof("mutate %q operation for object %q", req.Operation, req.Object)

	status := &admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}

	// only mutate the request for managedcluster
	if req.Resource.Group != "cluster.open-cluster-management.io" ||
		req.Resource.Resource != "managedclusters" {
		return status
	}

	// only mutate create and update operation
	if req.Operation != admissionv1beta1.Create && req.Operation != admissionv1beta1.Update {
		return status
	}

	managedCluster := &clusterv1.ManagedCluster{}
	if err := json.Unmarshal(req.Object.Raw, managedCluster); err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	// If LeaseDurationSeconds value is zero, update it to 60 by default
	if managedCluster.Spec.LeaseDurationSeconds == 0 {
		status.Patch = []byte(defaultLeaseDurationSecondsPatch)
		pt := admissionv1beta1.PatchTypeJSONPatch
		status.PatchType = &pt
	}

	return status
}

// Initialize is called by generic-admission-server on startup to setup initialization that managedclusters webhook needs.
func (a *ManagedClusterMutatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	// do nothing
	return nil
}
