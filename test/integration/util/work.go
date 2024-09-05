package util

import (
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	KubeDriver = "kube"
	MQTTDriver = "mqtt"
)

func NewWorkPatch(old, new *workapiv1.ManifestWork) ([]byte, error) {
	oldData, err := json.Marshal(old)
	if err != nil {
		return nil, err
	}

	newData, err := json.Marshal(new)
	if err != nil {
		return nil, err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return nil, err
	}

	return patchBytes, nil
}

func AppliedManifestWorkName(sourceDriver, hubHash string, work *workapiv1.ManifestWork) string {
	if sourceDriver != KubeDriver {
		// if the source is not kube, the uid will be used as the manifestwork name on the agent side
		return fmt.Sprintf("%s-%s", hubHash, work.UID)
	}

	return fmt.Sprintf("%s-%s", hubHash, work.Name)
}
