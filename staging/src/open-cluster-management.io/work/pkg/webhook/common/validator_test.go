package common

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	workv1 "open-cluster-management.io/api/work/v1"
	"reflect"
	"testing"
)

func newManifest(size int) workv1.Manifest {
	data := ""
	for i := 0; i < size; i++ {
		data += "a"
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"namespace": "test",
				"name":      "test",
			},
			"data": data,
		},
	}
	objectStr, _ := obj.MarshalJSON()
	manifest := workv1.Manifest{}
	manifest.Raw = objectStr
	return manifest
}
func Test_Validator(t *testing.T) {
	cases := []struct {
		name          string
		manifests     []workv1.Manifest
		expectedError error
	}{
		{
			name:          "not exceed the limit",
			manifests:     []workv1.Manifest{newManifest(100 * 1024), newManifest(100 * 1024)},
			expectedError: nil,
		},
		{
			name:          "exceed the limit",
			manifests:     []workv1.Manifest{newManifest(300 * 1024), newManifest(200 * 1024)},
			expectedError: fmt.Errorf("the size of manifests is 512192 bytes which exceeds the 512000 limit"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ManifestValidator.ValidateManifests(c.manifests)
			if !reflect.DeepEqual(err, c.expectedError) {
				t.Errorf("expected %#v but got: %#v", c.expectedError, err)
			}
		})
	}
}
