package agent

import (
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

// -------------------------------------------------------------------------
// Registration intent types
// -------------------------------------------------------------------------

// RegistrationConfig expresses the addon's registration intent for one
// credential.  Set exactly one of KubeClient or CustomSigner; the framework
// derives the v1beta1 Type discriminator from whichever field is non-nil.
type RegistrationConfig interface {
	RegistrationAPI() addonv1beta1.RegistrationConfig
}

// KubeClientRegistration configures kubeClient-type registration.
// Note: User and Groups might be overridden when the platform uses token as the driver.
type KubeClientRegistration struct {
	User   string
	Groups []string
}

func (c *KubeClientRegistration) RegistrationAPI() addonv1beta1.RegistrationConfig {
	registration := addonv1beta1.RegistrationConfig{
		Type: addonv1beta1.KubeClient,
	}

	if c.User != "" || len(c.Groups) > 0 {
		registration.KubeClient = &addonv1beta1.KubeClientConfig{
			Subject: addonv1beta1.KubeClientSubject{
				BaseSubject: addonv1beta1.BaseSubject{
					User:   c.User,
					Groups: c.Groups,
				},
			},
		}
	}
	return registration
}

// CustomSignerRegistration configures custom-signer-type registration.
// The framework always writes Subject as provided; no driver logic applies.
type CustomSignerRegistration struct {
	// SignerName is the name of the custom signer
	// (e.g. "example.io/my-signer").
	SignerName string

	User              string
	Groups            []string
	OrganizationUnits []string
}

func (c *CustomSignerRegistration) RegistrationAPI() addonv1beta1.RegistrationConfig {
	return addonv1beta1.RegistrationConfig{
		Type: addonv1beta1.CustomSigner,
		CustomSigner: &addonv1beta1.CustomSignerConfig{
			SignerName: c.SignerName,
			Subject: addonv1beta1.Subject{
				BaseSubject: addonv1beta1.BaseSubject{
					User:   c.User,
					Groups: c.Groups,
				},
				OrganizationUnits: c.OrganizationUnits,
			},
		},
	}
}
