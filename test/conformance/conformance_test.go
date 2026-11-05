// Copyright Contributors to the Open Cluster Management project

package conformance_test

import (
	"testing"

	"sigs.k8s.io/cluster-inventory-api/conformance"
)

// The shared observer uses the namespace prepared by OCM's own registration
// and ManagedClusterSetBinding APIs. It never substitutes seeded profiles.
func TestClusterInventoryConformance(t *testing.T) {
	conformance.TestConformance(t)
}
