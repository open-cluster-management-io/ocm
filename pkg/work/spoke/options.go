package spoke

import (
	"time"

	"github.com/spf13/pflag"

	"open-cluster-management.io/ocm/pkg/work/spoke/objectreader"
)

const (
	defaultUserAgent = "work-agent"
)

// WorkloadAgentOptions defines the flags for workload agent
type WorkloadAgentOptions struct {
	StatusSyncInterval                     time.Duration
	AppliedManifestWorkEvictionGracePeriod time.Duration
	MaxJSONRawLength                       int32
	WorkloadSourceDriver                   string
	WorkloadSourceConfig                   string
	CloudEventsClientID                    string
	CloudEventsClientCodecs                []string
	DefaultUserAgent                       string

	ObjectReaderOption *objectreader.Options
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewWorkloadAgentOptions() *WorkloadAgentOptions {
	return &WorkloadAgentOptions{
		MaxJSONRawLength:                       1024,
		StatusSyncInterval:                     10 * time.Second,
		AppliedManifestWorkEvictionGracePeriod: 60 * time.Minute,
		WorkloadSourceDriver:                   "kube",
		WorkloadSourceConfig:                   "/spoke/hub-kubeconfig/kubeconfig",
		DefaultUserAgent:                       defaultUserAgent,
		ObjectReaderOption:                     objectreader.NewOptions(),
	}
}

// AddFlags register and binds the default flags
func (o *WorkloadAgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.Int32Var(&o.MaxJSONRawLength, "max-json-raw-length",
		o.MaxJSONRawLength, "The maximum size of the JSON raw string returned from status feedback")
	fs.DurationVar(&o.StatusSyncInterval, "status-sync-interval",
		o.StatusSyncInterval, "Interval to sync resource status to hub.")
	fs.DurationVar(&o.AppliedManifestWorkEvictionGracePeriod, "appliedmanifestwork-eviction-grace-period",
		o.AppliedManifestWorkEvictionGracePeriod, "Grace period for appliedmanifestwork eviction")
	fs.StringVar(&o.WorkloadSourceDriver, "workload-source-driver",
		o.WorkloadSourceDriver, "The type of workload source driver, currently it can be kube, mqtt, grpc or kafka")
	fs.StringVar(&o.WorkloadSourceConfig, "workload-source-config",
		o.WorkloadSourceConfig, "The config file path of current workload source")
	fs.StringVar(&o.CloudEventsClientID, "cloudevents-client-id",
		o.CloudEventsClientID, "The ID of the cloudevents client when workload source source is based on cloudevents")
	fs.StringSliceVar(&o.CloudEventsClientCodecs, "cloudevents-client-codecs", o.CloudEventsClientCodecs,
		"The codecs for cloudevents client when workload source source is based on cloudevents, the valid codecs: manifest or manifestbundle")

	o.ObjectReaderOption.AddFlags(fs)
}
