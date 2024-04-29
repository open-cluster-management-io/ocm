package hub

import (
	"github.com/spf13/pflag"
)

// WorkHubManagerOptions defines the flags for work hub manager
type WorkHubManagerOptions struct {
	WorkDriver       string
	WorkDriverConfig string

	CloudEventsClientID string
}

func NewWorkHubManagerOptions() *WorkHubManagerOptions {
	return &WorkHubManagerOptions{
		WorkDriver: "kube",
	}
}

// AddFlags register and binds the default flags
func (o *WorkHubManagerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.WorkDriver, "work-driver",
		o.WorkDriver, "The type of work driver, currently it can be kube, mqtt or grpc")
	fs.StringVar(&o.WorkDriverConfig, "work-driver-config",
		o.WorkDriverConfig, "The config file path of current work driver")
	fs.StringVar(&o.CloudEventsClientID, "cloudevents-client-id",
		o.CloudEventsClientID, "The ID of the cloudevents client when publishing works with cloudevents")
}
