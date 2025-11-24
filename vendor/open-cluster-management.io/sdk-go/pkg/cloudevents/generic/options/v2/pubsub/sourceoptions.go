package pubsub

import (
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
)

// NewSourceOptions creates a new CloudEventsSourceOptions for Pub/Sub.
func NewSourceOptions(pubsubOptions *PubSubOptions,
	sourceID string) *options.CloudEventsSourceOptions {
	return &options.CloudEventsSourceOptions{
		CloudEventsTransport: &pubsubTransport{
			PubSubOptions: *pubsubOptions,
			sourceID:      sourceID,
			errorChan:     make(chan error),
		},
		SourceID: sourceID,
	}
}
