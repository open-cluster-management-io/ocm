// Copyright Contributors to the Open Cluster Management project
package v1

func (k *Klusterlet) GetResourceRequirement() *ResourceRequirement {
	return k.Spec.ResourceRequirement
}

// GetAgentIDTemplate returns agentIDTemplate, or DefaultSPIFFEAgentIDTemplate when it is empty
// or the config is nil.
func (c *KlusterletSPIFFEConfig) GetAgentIDTemplate() string {
	if c == nil || c.AgentIDTemplate == "" {
		return DefaultSPIFFEAgentIDTemplate
	}
	return c.AgentIDTemplate
}

// GetAddOnAgentIDTemplate returns addOnAgentIDTemplate, or DefaultSPIFFEAddOnAgentIDTemplate
// when it is empty or the config is nil.
func (c *KlusterletSPIFFEConfig) GetAddOnAgentIDTemplate() string {
	if c == nil || c.AddOnAgentIDTemplate == "" {
		return DefaultSPIFFEAddOnAgentIDTemplate
	}
	return c.AddOnAgentIDTemplate
}
