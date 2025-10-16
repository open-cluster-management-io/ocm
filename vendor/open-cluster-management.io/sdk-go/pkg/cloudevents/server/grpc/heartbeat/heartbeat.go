package heartbeat

import (
	"context"
	cecontext "github.com/cloudevents/sdk-go/v2/context"
	"github.com/google/uuid"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"time"
)

// Heartbeater is to periodically send heartbeat event to the output channel.
type Heartbeater struct {
	output   chan *pbv1.CloudEvent
	interval time.Duration
}

func NewHeartbeater(interval time.Duration, cacheSize int) *Heartbeater {
	return &Heartbeater{
		output:   make(chan *pbv1.CloudEvent, cacheSize),
		interval: interval,
	}
}

func (h *Heartbeater) Start(ctx context.Context) {
	logger := cecontext.LoggerFrom(ctx)

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			heartbeat := &pbv1.CloudEvent{
				SpecVersion: "1.0",
				Id:          uuid.New().String(),
				Type:        types.HeartbeatCloudEventsType,
			}

			select {
			case h.output <- heartbeat:
			default:
				logger.Warn("send channel is full, dropping heartbeat")
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *Heartbeater) Heartbeat() chan *pbv1.CloudEvent {
	return h.output
}
