// Copyright Contributors to the Open Cluster Management project
package v1

func (cm *ClusterManager) GetResourceRequirement() *ResourceRequirement {
	return cm.Spec.ResourceRequirement
}

// GetAgentTrustDomains returns the trust domains the hub accepts agents from. The hub's own
// trustDomain is always included and comes first, whether or not agentTrustDomains lists it;
// the listed trust domains follow in their order, without duplicates or empty entries.
// It returns nil for a nil config or an empty trustDomain.
//
// Example: trustDomain ocm-hub.example.org with agentTrustDomains [east.example.org,
// west.example.org] gives [ocm-hub.example.org east.example.org west.example.org].
func (c *SPIFFERegistrationConfig) GetAgentTrustDomains() []string {
	if c == nil || c.TrustDomain == "" {
		return nil
	}
	domains := make([]string, 0, len(c.AgentTrustDomains)+1)
	seen := make(map[string]bool, len(c.AgentTrustDomains)+1)
	for _, td := range append([]string{c.TrustDomain}, c.AgentTrustDomains...) {
		if td == "" || seen[td] {
			continue
		}
		seen[td] = true
		domains = append(domains, td)
	}
	return domains
}

// GetAgentIDTemplate returns agentIDTemplate, or DefaultSPIFFEAgentIDTemplate when it is empty
// or the config is nil.
func (c *SPIFFERegistrationConfig) GetAgentIDTemplate() string {
	if c == nil || c.AgentIDTemplate == "" {
		return DefaultSPIFFEAgentIDTemplate
	}
	return c.AgentIDTemplate
}

// GetAddOnAgentIDTemplate returns addOnAgentIDTemplate, or DefaultSPIFFEAddOnAgentIDTemplate
// when it is empty or the config is nil.
func (c *SPIFFERegistrationConfig) GetAddOnAgentIDTemplate() string {
	if c == nil || c.AddOnAgentIDTemplate == "" {
		return DefaultSPIFFEAddOnAgentIDTemplate
	}
	return c.AddOnAgentIDTemplate
}
