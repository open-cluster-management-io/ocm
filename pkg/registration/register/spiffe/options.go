// Package spiffe is the spiffe registration driver of the Klusterlet agents: they reach the hub's
// SPIFFE listener with their SPIRE-issued X.509-SVID only. There is no bootstrap kubeconfig, no
// CSR and no hub-kubeconfig Secret.
package spiffe

import (
	commonspiffe "open-cluster-management.io/ocm/pkg/common/spiffe"
)

// Option is the agents' shared SPIFFE settings.
type Option = commonspiffe.Option

// DefaultWorkloadAPISocket is where the SPIFFE CSI driver mounts the SPIRE agent's Workload API.
const DefaultWorkloadAPISocket = commonspiffe.DefaultWorkloadAPISocket

// NewOption returns the default settings.
func NewOption() *Option { return commonspiffe.NewOption() }
