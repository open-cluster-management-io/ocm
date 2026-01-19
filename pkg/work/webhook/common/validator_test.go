package common

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	workv1 "open-cluster-management.io/api/work/v1"
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

func newManifestWithNameAndNamespace(name, namespace string) workv1.Manifest {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
	objectStr, _ := obj.MarshalJSON()
	manifest := workv1.Manifest{}
	manifest.Raw = objectStr
	return manifest
}

func newManifestWithKind(name, namespace, kind string) workv1.Manifest {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
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
			name:          "duplicate manifests from newManifest helper",
			manifests:     []workv1.Manifest{newManifest(100 * 1024), newManifest(100 * 1024)},
			expectedError: fmt.Errorf("duplicate manifest for resource test/test with resource type v1.Secret"),
		},
		{
			name:          "exceed the limit",
			manifests:     []workv1.Manifest{newManifest(300 * 1024), newManifest(200 * 1024)},
			expectedError: fmt.Errorf("the size of manifests is 512192 bytes which exceeds the 512000 limit"),
		},
		{
			name: "duplicate manifests",
			manifests: []workv1.Manifest{
				newManifestWithNameAndNamespace("test1", "default"),
				newManifestWithNameAndNamespace("test2", "default"),
				newManifestWithNameAndNamespace("test1", "default"),
			},
			expectedError: fmt.Errorf("duplicate manifest for resource default/test1 with resource type v1.ConfigMap"),
		},
		{
			name: "same name different namespace",
			manifests: []workv1.Manifest{
				newManifestWithNameAndNamespace("test", "ns1"),
				newManifestWithNameAndNamespace("test", "ns2"),
			},
			expectedError: nil,
		},
		{
			name: "same name different kind",
			manifests: []workv1.Manifest{
				newManifestWithKind("test", "default", "ConfigMap"),
				newManifestWithKind("test", "default", "Secret"),
			},
			expectedError: nil,
		},
		{
			name: "unique manifests",
			manifests: []workv1.Manifest{
				newManifestWithNameAndNamespace("cm1", "default"),
				newManifestWithNameAndNamespace("cm2", "default"),
				newManifestWithNameAndNamespace("cm3", "default"),
			},
			expectedError: nil,
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
