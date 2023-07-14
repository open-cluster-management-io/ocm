package spoke

import (
	"time"

	"github.com/spf13/pflag"
)

// WorkloadAgentOptions defines the flags for workload agent
type WorkloadAgentOptions struct {
	StatusSyncInterval                     time.Duration
	AppliedManifestWorkEvictionGracePeriod time.Duration
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewWorkloadAgentOptions() *WorkloadAgentOptions {
	return &WorkloadAgentOptions{
		StatusSyncInterval:                     10 * time.Second,
		AppliedManifestWorkEvictionGracePeriod: 60 * time.Minute,
	}
}

// AddFlags register and binds the default flags
func (o *WorkloadAgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&o.StatusSyncInterval, "status-sync-interval", o.StatusSyncInterval, "Interval to sync resource status to hub.")
	fs.DurationVar(&o.AppliedManifestWorkEvictionGracePeriod, "appliedmanifestwork-eviction-grace-period",
		o.AppliedManifestWorkEvictionGracePeriod, "Grace period for appliedmanifestwork eviction")
}
