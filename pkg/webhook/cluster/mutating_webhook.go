package cluster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/helpers"
)

var nowFunc = time.Now
var defaultClusterSetName = "default"

type jsonPatchOperation struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value,omitempty"`
}

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

	var jsonPatches []jsonPatchOperation

	// set timeAdded of taint if it is nil and reset it if it is modified
	taintJsonPatches, status := a.processTaints(managedCluster, req.OldObject.Raw)
	if !status.Allowed {
		return status
	}
	jsonPatches = append(jsonPatches, taintJsonPatches...)

	if utilfeature.DefaultMutableFeatureGate.Enabled(features.DefaultClusterSet) {
		labelJsonPatches, status := a.addDefaultClusterSetLabel(managedCluster, req.Object.Raw)
		if !status.Allowed {
			return status
		}
		jsonPatches = append(jsonPatches, labelJsonPatches...)
	}

	if len(jsonPatches) == 0 {
		return status
	}

	patch, err := json.Marshal(jsonPatches)
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusInternalServerError, Reason: metav1.StatusReasonInternalError,
			Message: err.Error(),
		}
		return status
	}

	status.Patch = patch
	pt := admissionv1beta1.PatchTypeJSONPatch
	status.PatchType = &pt
	return status
}

//addDefaultClusterSetLabel add label "cluster.open-cluster-management.io/clusterset:default" for ManagedCluster if the managedCluster has no ManagedClusterSet label
func (a *ManagedClusterMutatingAdmissionHook) addDefaultClusterSetLabel(managedCluster *clusterv1.ManagedCluster, clusterObj []byte) ([]jsonPatchOperation, *admissionv1beta1.AdmissionResponse) {
	var jsonPatches []jsonPatchOperation

	status := &admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}

	if len(managedCluster.Labels) == 0 {
		jsonPatches = []jsonPatchOperation{
			{
				Operation: "add",
				Path:      "/metadata/labels",
				Value: map[string]string{
					clusterSetLabel: defaultClusterSetName,
				},
			},
		}
		return jsonPatches, status
	}

	clusterSetName, ok := managedCluster.Labels[clusterSetLabel]
	// Clusterset label do not exist
	if !ok {
		jsonPatches = []jsonPatchOperation{
			{
				Operation: "add",
				// there is a "/" in clusterset label. So need to transfer the "/" to "~1".
				Path:  "/metadata/labels/cluster.open-cluster-management.io~1clusterset",
				Value: defaultClusterSetName,
			},
		}
		return jsonPatches, status
	}

	// The clusterset label's value is "", update it to "default"
	if len(clusterSetName) == 0 {
		jsonPatches = []jsonPatchOperation{
			{
				Operation: "update",
				// there is a "/" in clusterset label. So need to transfer the "/" to "~1".
				Path:  "/metadata/labels/cluster.open-cluster-management.io~1clusterset",
				Value: defaultClusterSetName,
			},
		}
		return jsonPatches, status
	}

	return nil, status
}

// processTaints generates json patched for cluster taints
func (a *ManagedClusterMutatingAdmissionHook) processTaints(managedCluster *clusterv1.ManagedCluster, oldManagedClusterRaw []byte) ([]jsonPatchOperation, *admissionv1beta1.AdmissionResponse) {
	status := &admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}

	if len(managedCluster.Spec.Taints) == 0 {
		return nil, status
	}

	var oldManagedCluster *clusterv1.ManagedCluster
	if len(oldManagedClusterRaw) > 0 {
		cluster := &clusterv1.ManagedCluster{}
		if err := json.Unmarshal(oldManagedClusterRaw, cluster); err != nil {
			status.Allowed = false
			status.Result = &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusInternalServerError, Reason: metav1.StatusReasonInternalError,
				Message: err.Error(),
			}
			return nil, status
		}
		oldManagedCluster = cluster
	}

	var invalidTaints []string
	var jsonPatches []jsonPatchOperation
	now := metav1.NewTime(nowFunc())
	for index, taint := range managedCluster.Spec.Taints {
		originalTaint := helpers.FindTaintByKey(oldManagedCluster, taint.Key)
		switch {
		case originalTaint == nil:
			// new taint
			if !taint.TimeAdded.IsZero() {
				invalidTaints = append(invalidTaints, taint.Key)
				continue
			}
			jsonPatches = append(jsonPatches, newTaintTimeAddedJsonPatch(index, now.Time))
		case originalTaint.Value == taint.Value && originalTaint.Effect == taint.Effect:
			// no change
			if !originalTaint.TimeAdded.Equal(&taint.TimeAdded) {
				invalidTaints = append(invalidTaints, taint.Key)
			}
		default:
			// taint's value/effect has changed
			if !taint.TimeAdded.IsZero() {
				invalidTaints = append(invalidTaints, taint.Key)
				continue
			}
			jsonPatches = append(jsonPatches, newTaintTimeAddedJsonPatch(index, now.Time))
		}
	}

	if len(invalidTaints) == 0 {
		return jsonPatches, status
	}

	status.Allowed = false
	status.Result = &metav1.Status{
		Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
		Message: fmt.Sprintf("It is not allowed to set TimeAdded of Taint %q.", strings.Join(invalidTaints, ",")),
	}
	return nil, status
}

// Initialize is called by generic-admission-server on startup to setup initialization that managedclusters webhook needs.
func (a *ManagedClusterMutatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	// do nothing
	return nil
}

func newTaintTimeAddedJsonPatch(index int, timeAdded time.Time) jsonPatchOperation {
	return jsonPatchOperation{
		Operation: "replace",
		Path:      fmt.Sprintf("/spec/taints/%d/timeAdded", index),
		Value:     timeAdded.UTC().Format(time.RFC3339),
	}
}
