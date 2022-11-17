package cluster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	ocmfeature "open-cluster-management.io/api/feature"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestManagedClusterMutate(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name                   string
		request                *admissionv1beta1.AdmissionRequest
		expectedResponse       *admissionv1beta1.AdmissionResponse
		allowUpdateAcceptField bool
	}{
		{
			name: "mutate non-managedclusters request",
			request: &admissionv1beta1.AdmissionRequest{
				Resource: metav1.GroupVersionResource{
					Group:    "test.open-cluster-management.io",
					Version:  "v1",
					Resource: "tests",
				},
			},
			expectedResponse: newAdmissionResponse(true).build(),
		},
		{
			name: "mutate deleting operation",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Delete,
			},
			expectedResponse: newAdmissionResponse(true).build(),
		},
		{
			name: "new taints",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, nil)).
					addTaint(newTaint("c", "d", clusterv1.TaintEffectPreferNoSelect, nil)).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: defaultClusterSetName}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				addJsonPatch(newTaintTimeAddedJsonPatch(0, now)).
				addJsonPatch(newTaintTimeAddedJsonPatch(1, now)).
				build(),
		},
		{
			name: "new taint with timeAdded specified",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, nil)).
					addTaint(newTaint("c", "d", clusterv1.TaintEffectPreferNoSelect, newTime(now, 0))).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: defaultClusterSetName}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				addJsonPatch(newTaintTimeAddedJsonPatch(0, now)).
				addJsonPatch(newTaintTimeAddedJsonPatch(1, now)).
				addWarning(fmt.Sprintf("The specified TimeAdded value of Taint %q is ignored: %s.", "c", newTime(now, 0).UTC().Format(time.RFC3339))).
				build(),
		},
		{
			name: "update taint",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				OldObject: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))).
					addTaint(newTaint("c", "d", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: defaultClusterSetName}).
					build(),
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))). // no change
					addTaint(newTaint("c", "d", clusterv1.TaintEffectNoSelectIfNew, nil)).                      // effect modified
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: defaultClusterSetName}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				addJsonPatch(newTaintTimeAddedJsonPatch(1, now)).
				build(),
		},
		{
			name: "taint update request denied",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				OldObject: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))).
					addTaint(newTaint("c", "d", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))).
					build(),
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, newTime(now, -20*time.Second))).      // timeAdded modified
					addTaint(newTaint("c", "d", clusterv1.TaintEffectNoSelectIfNew, newTime(now, -10*time.Second))). // effect modified with timeAdded
					build(),
			},
			expectedResponse: newAdmissionResponse(false).
				withResult(metav1.StatusFailure, http.StatusBadRequest, metav1.StatusReasonBadRequest, "It is not allowed to set TimeAdded of Taint \"a,c\".").
				build(),
		},
		{
			name: "delete taint",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				OldObject: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))).
					addTaint(newTaint("c", "d", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: defaultClusterSetName}).
					build(),
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addTaint(newTaint("a", "b", clusterv1.TaintEffectNoSelect, newTime(now, -10*time.Second))).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: defaultClusterSetName}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).build(),
		},
		{
			name: "no label in cluster",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				addJsonPatch(jsonPatchOperation{
					Operation: "add",
					Path:      "/metadata/labels",
					Value: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				}).
				build(),
		},
		{
			name: "has other clusterset label",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: "c1"}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				build(),
		},
		{
			name: "has default clusterset label",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: defaultClusterSetName}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				build(),
		},
		{
			name: "has null clusterset label",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addLabels(map[string]string{clusterv1beta2.ClusterSetLabel: ""}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				addJsonPatch(jsonPatchOperation{
					Operation: "replace",
					Path:      "/metadata/labels/cluster.open-cluster-management.io~1clusterset",
					Value:     defaultClusterSetName,
				}).
				build(),
		},
		{
			name: "has other label in cluster",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  managedclustersSchema,
				Operation: admissionv1beta1.Create,
				Object: newManagedCluster().
					withLeaseDurationSeconds(60).
					addLabels(map[string]string{"k": "v"}).
					build(),
			},
			expectedResponse: newAdmissionResponse(true).
				addJsonPatch(jsonPatchOperation{
					Operation: "add",
					Path:      "/metadata/labels/cluster.open-cluster-management.io~1clusterset",
					Value:     defaultClusterSetName,
				}).
				build(),
		},
	}

	nowFunc = func() time.Time {
		return now
	}
	utilruntime.Must(utilfeature.DefaultMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	if err := utilfeature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=true", string(ocmfeature.DefaultClusterSet))); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			admissionHook := &ManagedClusterMutatingAdmissionHook{}
			actualResponse := admissionHook.Admit(c.request)
			if !reflect.DeepEqual(actualResponse, c.expectedResponse) {
				t.Errorf("expected \n%#v but got: \n%#v", c.expectedResponse, actualResponse)
			}
		})
	}
}

type admissionResponseBuilder struct {
	jsonPatchOperations []jsonPatchOperation
	response            admissionv1beta1.AdmissionResponse
}

func newAdmissionResponse(allowed bool) *admissionResponseBuilder {
	return &admissionResponseBuilder{
		response: admissionv1beta1.AdmissionResponse{
			Allowed: allowed,
		},
	}
}

func (b *admissionResponseBuilder) addJsonPatch(jsonPatch jsonPatchOperation) *admissionResponseBuilder {
	b.jsonPatchOperations = append(b.jsonPatchOperations, jsonPatch)
	pt := admissionv1beta1.PatchTypeJSONPatch
	b.response.PatchType = &pt
	return b
}

func (b *admissionResponseBuilder) withResult(status string, code int32, reason metav1.StatusReason, message string) *admissionResponseBuilder {
	b.response.Result = &metav1.Status{
		Status:  status,
		Code:    code,
		Reason:  reason,
		Message: message,
	}
	return b
}

func (b *admissionResponseBuilder) addWarning(warning string) *admissionResponseBuilder {
	b.response.Warnings = append(b.response.Warnings, warning)
	return b
}

func (b *admissionResponseBuilder) build() *admissionv1beta1.AdmissionResponse {
	if len(b.jsonPatchOperations) > 0 {
		patch, _ := json.Marshal(b.jsonPatchOperations)
		b.response.Patch = patch
	}
	return &b.response
}

type managedClusterBuilder struct {
	cluster clusterv1.ManagedCluster
}

func newManagedCluster() *managedClusterBuilder {
	return &managedClusterBuilder{
		cluster: *testinghelpers.NewManagedCluster(),
	}
}

func (b *managedClusterBuilder) withLeaseDurationSeconds(leaseDurationSeconds int32) *managedClusterBuilder {
	b.cluster.Spec.LeaseDurationSeconds = leaseDurationSeconds
	return b
}

func (b *managedClusterBuilder) addTaint(taint clusterv1.Taint) *managedClusterBuilder {
	b.cluster.Spec.Taints = append(b.cluster.Spec.Taints, taint)
	return b
}
func (b *managedClusterBuilder) addLabels(labels map[string]string) *managedClusterBuilder {
	var modified bool
	resourcemerge.MergeMap(&modified, &b.cluster.Labels, labels)
	return b
}

func (b *managedClusterBuilder) build() runtime.RawExtension {
	clusterObj, _ := json.Marshal(b.cluster)
	return runtime.RawExtension{
		Raw: clusterObj,
	}
}

func newTaint(key, value string, effect clusterv1.TaintEffect, timeAdded *metav1.Time) clusterv1.Taint {
	taint := clusterv1.Taint{
		Key:    key,
		Value:  value,
		Effect: effect,
	}

	if timeAdded != nil {
		taint.TimeAdded = *timeAdded
	}

	return taint
}

func newTime(time time.Time, offset time.Duration) *metav1.Time {
	mt := metav1.NewTime(time.Add(offset))
	return &mt
}
