package spiffe

import (
	"context"
	"fmt"
	"path"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// HubDriver is the hub side of the spiffe driver. The agents' identities come from SPIRE, so
// there is nothing to approve or sign; the driver only decides automatic acceptance.
type HubDriver struct {
	autoApprovedIdentities []string
}

var _ register.HubDriver = &HubDriver{}

// NewHubDriver returns the spiffe hub driver. autoApprovedIdentities are SPIFFE IDs in which "*"
// matches one whole path segment.
func NewHubDriver(autoApprovedIdentities []string) (*HubDriver, error) {
	if err := ValidateAutoApprovedIdentities(autoApprovedIdentities); err != nil {
		return nil, err
	}
	return &HubDriver{autoApprovedIdentities: autoApprovedIdentities}, nil
}

// ValidateAutoApprovedIdentities checks that every entry is a usable pattern.
func ValidateAutoApprovedIdentities(ids []string) error {
	for _, id := range ids {
		if _, err := path.Match(id, ""); err != nil {
			return fmt.Errorf("auto-approved SPIFFE ID %q: %w", id, err)
		}
	}
	return nil
}

// CreatePermissions has nothing to do: the hub RBAC of the cluster's agents is created by the
// registration controller, as for every driver.
func (d *HubDriver) CreatePermissions(context.Context, *clusterv1.ManagedCluster) error { return nil }

// Run has nothing to run: there is no CSR to approve.
func (d *HubDriver) Run(context.Context, int) {}

// Cleanup has nothing to clean: the driver creates no resources.
func (d *HubDriver) Cleanup(context.Context, *clusterv1.ManagedCluster) error { return nil }

// Accept decides automatic acceptance (with the ManagedClusterAutoApproval feature gate), as
// the csr and grpc drivers do for their users. A cluster that a spiffe agent registered, which
// the hub records in the spiffe-registered-by annotation, is accepted only if that SPIFFE ID
// matches an auto-approved identity; with none configured, never. Any other cluster isn't this
// driver's to decide.
func (d *HubDriver) Accept(cluster *clusterv1.ManagedCluster) bool {
	registeredBy, isSPIFFE := cluster.Annotations[clusterv1.SPIFFERegisteredByAnnotationKey]
	if !isSPIFFE {
		return true
	}
	for _, pattern := range d.autoApprovedIdentities {
		if matched, _ := path.Match(pattern, registeredBy); matched {
			return true
		}
	}
	return false
}
