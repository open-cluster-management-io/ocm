package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	apiruntime "k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/util/runtime"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/registration/pkg/features"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

func TestDefault(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name          string
		cluster       *clusterv1.ManagedCluster
		oldCluster    *clusterv1.ManagedCluster
		expectCluster *clusterv1.ManagedCluster
		expectedError bool
	}{
		{
			name:          "Empty spec cluster",
			expectedError: false,
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
			},
		},
		{
			name:          "New taint",
			expectedError: false,
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:    "a",
							Value:  "b",
							Effect: clusterv1.TaintEffectNoSelect,
						},
						{
							Key:    "c",
							Value:  "d",
							Effect: clusterv1.TaintEffectPreferNoSelect,
						},
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, 0),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectPreferNoSelect,
							TimeAdded: newTime(now, 0),
						},
					},
				},
			},
		},
		{
			name:          "New taint with timeAdded specified",
			expectedError: false,
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:    "a",
							Value:  "b",
							Effect: clusterv1.TaintEffectNoSelect,
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectPreferNoSelect,
							TimeAdded: newTime(now, 0),
						},
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, 0),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectPreferNoSelect,
							TimeAdded: newTime(now, 0),
						},
					},
				},
			},
		},
		{
			name:          "update taint",
			expectedError: false,
			oldCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
					},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
						{
							Key:    "c",
							Value:  "d",
							Effect: clusterv1.TaintEffectNoSelectIfNew,
						},
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectNoSelectIfNew,
							TimeAdded: newTime(now, 0),
						},
					},
				},
			},
		},
		{
			name:          "taint update request denied",
			expectedError: true,
			oldCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
					},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -20*time.Second),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectNoSelectIfNew,
							TimeAdded: newTime(now, -10*time.Second),
						},
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectNoSelectIfNew,
							TimeAdded: newTime(now, 0),
						},
					},
				},
			},
		},
		{
			name:          "delete taint",
			expectedError: false,
			oldCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
						{
							Key:       "c",
							Value:     "d",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
					},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:       "a",
							Value:     "b",
							Effect:    clusterv1.TaintEffectNoSelect,
							TimeAdded: newTime(now, -10*time.Second),
						},
					},
				},
			},
		},
		{
			name:          "Has clusterset label",
			expectedError: false,
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: "s1",
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: "s1",
					},
				},
			},
		},
		{
			name:          "Has default clusterset label",
			expectedError: false,
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
			},
		},
		{
			name:          "Has null clusterset label",
			expectedError: false,
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: "",
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
			},
		},
		{
			name:          "Has other label",
			expectedError: false,
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						"k": "v",
					},
				},
			},
			expectCluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "set-1",
					Labels: map[string]string{
						"k":                            "v",
						clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
					},
				},
			},
		},
	}
	runtime.Must(features.DefaultHubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	if err := features.DefaultHubMutableFeatureGate.Set(fmt.Sprintf("%s=true", string(ocmfeature.DefaultClusterSet))); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := ManagedClusterWebhook{}
			var oldClusterBytes []byte
			if c.oldCluster == nil {
				oldClusterBytes = []byte{}
			} else {
				oldClusterBytes, _ = json.Marshal(c.oldCluster)
			}
			clusterBytes, _ := json.Marshal(c.cluster)
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Resource: metav1.GroupVersionResource{
						Group:    "test.open-cluster-management.io",
						Version:  "v1",
						Resource: "tests",
					},
					OldObject: apiruntime.RawExtension{
						Raw: oldClusterBytes,
					},
					Object: apiruntime.RawExtension{
						Raw: clusterBytes,
					},
				},
			}
			ctx := admission.NewContextWithRequest(context.Background(), req)

			cluster := &clusterv1.ManagedCluster{}
			if err := json.Unmarshal(clusterBytes, cluster); err != nil {
				t.Errorf("faile to decode cluster %s", string(clusterBytes))
			}
			err := w.Default(ctx, cluster)
			if err != nil || c.expectCluster != nil {
				if err != nil && !c.expectedError {
					t.Errorf("Case:%v, Expect nil but got Error, err: %v", c.name, err)
				}
				if err == nil && c.expectedError {
					t.Errorf("Case:%v, Expect Error but got nil", c.name)
				}
				return
			}
			if !reflect.DeepEqual(cluster.Labels, c.expectCluster.Labels) {
				t.Errorf("Case:%v, Expect cluster label is not same as return cluster. expect:%v,return:%v", c.name, c.expectCluster.Labels, c.cluster.Labels)
			}
			if !DiffTaintTime(cluster.Spec.Taints, c.expectCluster.Spec.Taints) {
				t.Errorf("Case:%v, Expect cluster taits:%v, return cluster taints:%v", c.name, c.expectCluster.Spec.Taints, c.cluster.Spec.Taints)
			}
		})
	}
}

func DiffTaintTime(src, dest []clusterv1.Taint) bool {
	if len(src) != len(dest) {
		return false
	}
	for k := range src {
		if src[k].TimeAdded.Minute() != dest[k].TimeAdded.Minute() || src[k].TimeAdded.Second() != dest[k].TimeAdded.Second() {
			fmt.Printf("src:%v, \ndest:%v\n", src[k].TimeAdded.String(), dest[k].TimeAdded.String())
			return false
		}
	}
	return true
}

func newTime(time time.Time, offset time.Duration) metav1.Time {
	mt := metav1.NewTime(time.Add(offset))
	return mt
}
