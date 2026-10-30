package crypto

import (
	configv1 "github.com/openshift/api/config/v1"
)

// ShouldHonorClusterTLSProfile returns true if the component should honor the
// cluster-wide TLS security profile settings from apiserver.config.openshift.io/cluster.
//
// When this returns true (StrictAllComponents mode), components must honor the
// cluster-wide TLS profile unless they have a component-specific TLS configuration
// that overrides it.
//
// Unknown enum values are treated as StrictAllComponents for forward compatibility
// and to default to the more secure behavior.
func ShouldHonorClusterTLSProfile(tlsAdherence configv1.TLSAdherencePolicy) bool {
	switch tlsAdherence {
	case configv1.TLSAdherencePolicyNoOpinion, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly:
		return false
	default:
		return true
	}
}

// GetSecurityProfileGroups returns the TLS group preference names for the resolved
// profile spec as []string. The strings match the TLSGroup constants from
// openshift/api and can be passed directly to operands that accept group names as
// CLI arguments.
//
// When the Go runtime is in FIPS mode, non-FIPS-approved groups are dropped
// before conversion so consuming components receive an already-safe list.
// See tls_fips.go for the allowlist and the removal TODO (Go 1.27 without OpenSSL FIPS).
func GetSecurityProfileGroups(profileSpec *configv1.TLSProfileSpec) []string {
	src := profileSpec.Groups
	if IsFIPSEnabled() {
		src = FIPSApprovedTLSGroups(src)
	}
	groups := make([]string, 0, len(src))
	for _, g := range src {
		groups = append(groups, string(g))
	}
	return groups
}
