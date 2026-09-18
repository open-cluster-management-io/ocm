package templateagent

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCoerceSpecReplicas(t *testing.T) {
	tests := []struct {
		name         string
		object       *unstructured.Unstructured
		wantReplicas interface{}
		wantType     string
	}{
		{
			name: "string replicas coerced to int64",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec":       map[string]interface{}{"replicas": "3"},
			}},
			wantReplicas: int64(3),
			wantType:     "int64",
		},
		{
			name: "already int64 left unchanged",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec":       map[string]interface{}{"replicas": int64(2)},
			}},
			wantReplicas: int64(2),
			wantType:     "int64",
		},
		{
			name: "invalid string leaves field unchanged",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec":       map[string]interface{}{"replicas": "abc"},
			}},
			wantReplicas: "abc",
			wantType:     "string",
		},
		{
			name: "missing replicas field is a no-op",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec":       map[string]interface{}{},
			}},
			wantReplicas: nil,
			wantType:     "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := coerceSpecReplicas(tt.object); err != nil {
				t.Fatalf("coerceSpecReplicas() unexpected error: %v", err)
			}
			val, _, _ := unstructured.NestedFieldNoCopy(tt.object.Object, "spec", "replicas")
			if val != tt.wantReplicas {
				t.Errorf("spec.replicas = %v (%T), want %v (%s)", val, val, tt.wantReplicas, tt.wantType)
			}
		})
	}
}

func TestCoerceKnownIntFields(t *testing.T) {
	tests := []struct {
		name        string
		object      *unstructured.Unstructured
		wantCoerced bool
	}{
		{
			name: "Deployment spec.replicas coerced",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec":       map[string]interface{}{"replicas": "5"},
			}},
			wantCoerced: true,
		},
		{
			name: "StatefulSet spec.replicas coerced",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "StatefulSet",
				"spec":       map[string]interface{}{"replicas": "2"},
			}},
			wantCoerced: true,
		},
		{
			name: "ConfigMap data string stays string (no coercion)",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"data": map[string]interface{}{
					"replicas": "3",
				},
			}},
			wantCoerced: false,
		},
		{
			name: "DaemonSet not in allow-list — no coercion",
			object: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "DaemonSet",
				"spec":       map[string]interface{}{"replicas": "1"},
			}},
			wantCoerced: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := coerceKnownIntFields(tt.object); err != nil {
				t.Fatalf("coerceKnownIntFields() unexpected error: %v", err)
			}

			gvk := tt.object.GroupVersionKind()
			if val, found, _ := unstructured.NestedFieldNoCopy(tt.object.Object, "spec", "replicas"); found {
				_, isInt := val.(int64)
				if tt.wantCoerced && !isInt {
					t.Errorf("%s spec.replicas should be int64, got %T", gvk.Kind, val)
				}
				if !tt.wantCoerced && isInt {
					t.Errorf("%s spec.replicas should NOT be int64, got %T", gvk.Kind, val)
				}
			}

			// ConfigMap data values must remain strings
			if gvk.Kind == "ConfigMap" {
				val, _, _ := unstructured.NestedFieldNoCopy(tt.object.Object, "data", "replicas")
				if _, isString := val.(string); !isString {
					t.Errorf("ConfigMap data value should remain string, got %T", val)
				}
			}
		})
	}
}
