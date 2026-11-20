package hub

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestHubManagerSPIFFEFlags(t *testing.T) {
	m := NewHubManagerOptions()
	fs := pflag.NewFlagSet("hub", pflag.ContinueOnError)
	m.AddFlags(fs)
	if err := fs.Parse([]string{"--spiffe-max-svid-lifetime=2h", "--spiffe-max-clock-skew=1m",
		"--enabled-registration-drivers=spiffe"}); err != nil {
		t.Fatal(err)
	}
	if m.SPIFFEMaxSVIDLifetime != 2*time.Hour || m.SPIFFEMaxClockSkew != time.Minute {
		t.Errorf("lifetime %s, skew %s", m.SPIFFEMaxSVIDLifetime, m.SPIFFEMaxClockSkew)
	}
	if len(m.EnabledRegistrationDrivers) != 1 || m.EnabledRegistrationDrivers[0] != "spiffe" {
		t.Errorf("drivers %v", m.EnabledRegistrationDrivers)
	}
}

func TestHubManagerSPIFFEAutoApprovedIdentitiesFlag(t *testing.T) {
	m := NewHubManagerOptions()
	fs := pflag.NewFlagSet("hub", pflag.ContinueOnError)
	m.AddFlags(fs)
	if err := fs.Parse([]string{"--spiffe-auto-approved-identities=spiffe://td/ocm/cluster/*/agent/registration,spiffe://td/x"}); err != nil {
		t.Fatal(err)
	}
	if len(m.SPIFFEAutoApprovedIdentities) != 2 {
		t.Errorf("identities %v", m.SPIFFEAutoApprovedIdentities)
	}
}
