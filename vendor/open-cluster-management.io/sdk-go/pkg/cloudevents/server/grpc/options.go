package grpc

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
)

// BrokerOptions contains configuration options for the GRPCBroker.
type BrokerOptions struct {
	// HeartbeatDisabled controls whether heartbeat mechanism is disabled.
	// Default: false (heartbeat is enabled by default)
	HeartbeatDisabled bool

	// HeartbeatCheckInterval is the interval for heartbeat checks.
	// Default: 10 seconds
	HeartbeatCheckInterval time.Duration

	// EventQueueSize is the buffer size of each subscriber's event queue.
	// Zero uses the default.
	// Default: 100
	EventQueueSize int

	// HeartbeatQueueSize is the buffer size of each subscriber's heartbeat queue.
	// Zero uses the default.
	// Default: 10
	HeartbeatQueueSize int
}

const (
	defaultEventQueueSize     = 100
	defaultHeartbeatQueueSize = 10
)

// NewBrokerOptions creates a new BrokerOptions with default values.
func NewBrokerOptions() *BrokerOptions {
	return &BrokerOptions{
		HeartbeatDisabled:      false,
		HeartbeatCheckInterval: 10 * time.Second,
		EventQueueSize:         defaultEventQueueSize,
		HeartbeatQueueSize:     defaultHeartbeatQueueSize,
	}
}

// AddFlags adds flags for configuring the broker options.
func (o *BrokerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.HeartbeatDisabled, "broker-heartbeat-disabled", o.HeartbeatDisabled,
		"Disable heartbeat mechanism for gRPC broker")
	fs.DurationVar(&o.HeartbeatCheckInterval, "broker-heartbeat-interval", o.HeartbeatCheckInterval,
		"Interval for heartbeat checks in gRPC broker")
	fs.IntVar(&o.EventQueueSize, "broker-event-queue-size", o.EventQueueSize,
		"Buffer size of each subscriber's event queue in gRPC broker")
	fs.IntVar(&o.HeartbeatQueueSize, "broker-heartbeat-queue-size", o.HeartbeatQueueSize,
		"Buffer size of each subscriber's heartbeat queue in gRPC broker")
}

// Validate checks the broker options for valid values.
func (o *BrokerOptions) Validate() error {
	// Validate heartbeat check interval if heartbeat is enabled
	if !o.HeartbeatDisabled && o.HeartbeatCheckInterval < 10*time.Second {
		return fmt.Errorf("heartbeat_check_interval (%v) must be at least 10 seconds when heartbeat is enabled", o.HeartbeatCheckInterval)
	}
	if o.EventQueueSize < 0 {
		return fmt.Errorf("event_queue_size (%d) must not be negative", o.EventQueueSize)
	}
	if o.HeartbeatQueueSize < 0 {
		return fmt.Errorf("heartbeat_queue_size (%d) must not be negative", o.HeartbeatQueueSize)
	}
	return nil
}
