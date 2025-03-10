package statushash

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	workv1 "open-cluster-management.io/api/work/v1"
)

// ManifestWorkStatusHash returns the SHA256 checksum of a ManifestWork status.
func ManifestWorkStatusHash(work *workv1.ManifestWork) (string, error) {
	statusBytes, err := json.Marshal(work.Status)
	if err != nil {
		return "", fmt.Errorf("failed to marshal work status, %v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(statusBytes)), nil
}
