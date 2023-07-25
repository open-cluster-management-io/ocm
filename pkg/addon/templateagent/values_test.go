package templateagent

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestGetAddOnRegistriesPrivateValuesFromClusterAnnotation(t *testing.T) {
	cases := []struct {
		name           string
		cluster        *clusterv1.ManagedCluster
		expectedValues addonfactory.Values
		expectedError  string
	}{
		{
			name:           "no values",
			cluster:        &clusterv1.ManagedCluster{},
			expectedValues: addonfactory.Values{},
		},
		{
			name: "not expected annotation",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ClusterImageRegistriesAnnotation: `{"registries-test":[{"mirror-test":"quay.io/ocm","source-test":"quay-test.io/ocm"}]}`,
					},
				},
			},
			expectedValues: addonfactory.Values{},
		},
		{
			name: "annotation invalid",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ClusterImageRegistriesAnnotation: `{"registries":`,
					},
				},
			},
			expectedValues: addonfactory.Values{},
			expectedError:  "unexpected end of JSON input",
		},
		{
			name: "override registries",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ClusterImageRegistriesAnnotation: `{"registries":[{"mirror":"quay.io/ocm","source":"quay-test.io/ocm"}]}`,
					},
				},
			},
			expectedValues: addonfactory.Values{
				RegistriesPrivateValueKey: []addonapiv1alpha1.ImageMirror{
					{
						Source: "quay-test.io/ocm",
						Mirror: "quay.io/ocm",
					},
				},
			},
		},
		{
			name: "override image",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ClusterImageRegistriesAnnotation: `{"registries":[{"mirror":"quay.io/ocm/test","source":"quay.io/open-cluster-management/test"}]}`,
					},
				},
			},
			expectedValues: addonfactory.Values{
				RegistriesPrivateValueKey: []addonapiv1alpha1.ImageMirror{
					{
						Source: "quay.io/open-cluster-management/test",
						Mirror: "quay.io/ocm/test",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			values, err := GetAddOnRegistriesPrivateValuesFromClusterAnnotation(c.cluster, nil)
			if err != nil || len(c.expectedError) > 0 {
				assert.ErrorContains(t, err, c.expectedError, "expected error: %v, got: %v", c.expectedError, err)
			}

			if !reflect.DeepEqual(values, c.expectedValues) {
				t.Errorf("expected values: %v, got: %v", c.expectedValues, values)
			}
		})
	}
}
