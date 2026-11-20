package factory

import (
	"strings"
	"testing"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

// The spiffe agent driver and the spiffe add-on driver go together, and only together.
func TestValidateSPIFFEDriversTogether(t *testing.T) {
	for _, c := range []struct{ agent, addOn string }{
		{operatorv1.SPIFFEAuthType, "csr"},
		{"csr", operatorv1.SPIFFEAuthType},
	} {
		o := NewOptions()
		o.RegistrationAuth, o.AddonKubeClientRegistrationAuth = c.agent, c.addOn
		if err := o.Validate(); err == nil || !strings.Contains(err.Error(), "goes with") {
			t.Errorf("%s with add-on driver %s: %v", c.agent, c.addOn, err)
		}
	}
}
