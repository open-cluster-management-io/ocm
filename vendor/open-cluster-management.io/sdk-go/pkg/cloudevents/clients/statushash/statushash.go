package statushash

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
)

// StatusHash returns the SHA256 checksum of a resource status.
func StatusHash[T generic.ResourceObject](resource T) (string, error) {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
	if err != nil {
		return "", err
	}

	status, found, err := unstructured.NestedMap(u, "status")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no status for the resource %s", resource.GetUID())
	}

	statusBytes, err := json.Marshal(status)
	if err != nil {
		return "", fmt.Errorf("failed to marshal resource status, %v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(statusBytes)), nil
}
