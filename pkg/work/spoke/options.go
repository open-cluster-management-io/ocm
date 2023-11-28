package spoke

import (
	"time"

	"github.com/spf13/pflag"
)

const (
	KubeDriver = "kube"
	MQTTDriver = "mqtt"
)

type WorkloadSourceDriver struct {
	Type   string
	Config string
}

// WorkloadAgentOptions defines the flags for workload agent
type WorkloadAgentOptions struct {
	StatusSyncInterval                     time.Duration
	AppliedManifestWorkEvictionGracePeriod time.Duration
	WorkloadSourceDriver                   WorkloadSourceDriver
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
	fs.DurationVar(&o.StatusSyncInterval, "status-sync-interval",
		o.StatusSyncInterval, "Interval to sync resource status to hub.")
	fs.DurationVar(&o.AppliedManifestWorkEvictionGracePeriod, "appliedmanifestwork-eviction-grace-period",
		o.AppliedManifestWorkEvictionGracePeriod, "Grace period for appliedmanifestwork eviction")
	fs.StringVar(&o.WorkloadSourceDriver.Type, "workload-source-driver",
		o.WorkloadSourceDriver.Type, "The type of workload source driver, currently it can be kube or mqtt")
	fs.StringVar(&o.WorkloadSourceDriver.Config, "workload-source-config",
		o.WorkloadSourceDriver.Config, "The config file path of current workload source")
}
