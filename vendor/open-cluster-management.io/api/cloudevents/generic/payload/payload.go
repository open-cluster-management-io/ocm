package payload

import (
	"encoding/json"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

type ResourceVersion struct {
	ResourceID      string `json:"resourceID"`
	ResourceVersion int64  `json:"resourceVersion"`
}

type ResourceStatusHash struct {
	ResourceID string `json:"resourceID"`
	StatusHash string `json:"statusHash"`
}

// ResourceVersionList represents the resource versions of the resources maintained by the agent.
// The item of this list includes the resource ID and resource version.
type ResourceVersionList struct {
	Versions []ResourceVersion `json:"resourceVersions"`
}

// ResourceStatusHashList represents the status hash of the resources maintained by the source.
// The item of this list includes the resource ID and resource status hash.
type ResourceStatusHashList struct {
	Hashes []ResourceStatusHash `json:"statusHashes"`
}

func DecodeSpecResyncRequest(evt cloudevents.Event) (*ResourceVersionList, error) {
	versions := &ResourceVersionList{}
	data := evt.Data()
	if err := json.Unmarshal(data, versions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec resync request payload %s, %v", string(data), err)
	}
	return versions, nil
}

func DecodeStatusResyncRequest(evt cloudevents.Event) (*ResourceStatusHashList, error) {
	hashes := &ResourceStatusHashList{}
	data := evt.Data()
	if err := json.Unmarshal(data, hashes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status resync request payload %s, %v", string(data), err)
	}
	return hashes, nil
}
