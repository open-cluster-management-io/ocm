package helpers

import (
	"reflect"
	"testing"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

func TestFilterClusterAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        map[string]string
	}{
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			want:        map[string]string{},
		},
		{
			name: "no cluster annotations",
			annotations: map[string]string{
				"foo": "bar",
				"baz": "qux",
			},
			want: map[string]string{},
		},
		{
			name: "one cluster annotation",
			annotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "foo": "bar",
				"baz": "qux",
			},
			want: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "foo": "bar",
			},
		},
		{
			name: "multiple cluster annotations",
			annotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "foo": "bar",
				operatorv1.ClusterAnnotationsKeyPrefix + "baz": "qux",
				"quux": "corge",
			},
			want: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "foo": "bar",
				operatorv1.ClusterAnnotationsKeyPrefix + "baz": "qux",
			},
		},
		{
			name: "all annotations are cluster annotations",
			annotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "foo": "bar",
				operatorv1.ClusterAnnotationsKeyPrefix + "baz": "qux",
			},
			want: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "foo": "bar",
				operatorv1.ClusterAnnotationsKeyPrefix + "baz": "qux",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FilterClusterAnnotations(tt.annotations); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterClusterAnnotations() = %v, want %v", got, tt.want)
			}
		})
	}
}
