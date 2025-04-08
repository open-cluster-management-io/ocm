package options

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// CloudEventsOptions provides cloudevents clients to send/receive cloudevents based on different event protocol.
//
// Available implementations:
//   - MQTT
//   - KAFKA
//   - gRPC
type CloudEventsOptions interface {
	// WithContext returns back a new context with the given cloudevent context. The new context will be used when
	// sending a cloudevent.The new context is protocol-dependent, for example, for MQTT, the new context should contain
	// the MQTT topic, for Kafka, the context should contain the message key, etc.
	WithContext(ctx context.Context, evtContext cloudevents.EventContext) (context.Context, error)

	// Protocol returns a specific protocol to initialize the cloudevents client.
	Protocol(ctx context.Context, dataType types.CloudEventsDataType) (CloudEventsProtocol, error)

	// ErrorChan returns a chan which will receive the cloudevents connection error. The source/agent client will try to
	// reconnect the when this error occurs.
	ErrorChan() <-chan error
}

// CloudEventsProtocol is a set of interfaces for a specific binding need to implemented
// Reference: https://cloudevents.github.io/sdk-go/protocol_implementations.html#protocol-interfaces
type CloudEventsProtocol interface {
	protocol.Sender
	protocol.Receiver
	protocol.Closer
}

// EventRateLimit for limiting the event sending rate.
type EventRateLimit struct {
	// QPS indicates the maximum QPS to send the event.
	// If it's less than or equal to zero, the DefaultQPS (50) will be used.
	QPS float32

	// Maximum burst for throttle.
	// If it's less than or equal to zero, the DefaultBurst (100) will be used.
	Burst int
}

// CloudEventsSourceOptions provides the required options to build a source CloudEventsClient
type CloudEventsSourceOptions struct {
	// CloudEventsOptions provides cloudevents clients to send/receive cloudevents based on different event protocol.
	CloudEventsOptions CloudEventsOptions

	// SourceID is a unique identifier for a source, for example, it can generate a source ID by hashing the hub cluster
	// URL and appending the controller name. Similarly, a RESTful service can select a unique name or generate a unique
	// ID in the associated database for its source identification.
	SourceID string

	// EventRateLimit limits the event sending rate.
	EventRateLimit EventRateLimit
}

// CloudEventsAgentOptions provides the required options to build an agent CloudEventsClient
type CloudEventsAgentOptions struct {
	// CloudEventsOptions provides cloudevents clients to send/receive cloudevents based on different event protocol.
	CloudEventsOptions CloudEventsOptions

	// AgentID is a unique identifier for an agent, for example, it can consist of a managed cluster name and an agent
	// name.
	AgentID string

	// ClusterName is the name of a managed cluster on which the agent runs.
	ClusterName string

	// EventRateLimit limits the event sending rate.
	EventRateLimit EventRateLimit
}
