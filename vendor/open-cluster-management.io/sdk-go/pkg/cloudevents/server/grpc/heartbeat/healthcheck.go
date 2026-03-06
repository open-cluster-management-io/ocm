package heartbeat

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"

	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
)

// HealthChecker receives heartbeat from heartbeatChan, if it does not receive heartbeat in healthinessTimout,
// sends a notification to the errChan
type HealthChecker struct {
	healthinessTimout time.Duration
	errChan           chan error
	heartbeatChan     chan *pbv1.CloudEvent
	enabled           bool
}

func NewHealthChecker(healthinessTimout *time.Duration, errChan chan error) *HealthChecker {
	if healthinessTimout == nil {
		return &HealthChecker{
			heartbeatChan: make(chan *pbv1.CloudEvent),
		}
	}
	return &HealthChecker{
		healthinessTimout: *healthinessTimout,
		errChan:           errChan,
		heartbeatChan:     make(chan *pbv1.CloudEvent),
		enabled:           true,
	}
}

func (hc *HealthChecker) Input() chan *pbv1.CloudEvent {
	return hc.heartbeatChan
}

func (hc *HealthChecker) Start(ctx context.Context) {
	if !hc.enabled {
		hc.bypass(ctx)
		return
	}

	hc.check(ctx)
}

func (hc *HealthChecker) check(ctx context.Context) {
	logger := klog.FromContext(ctx)
	// if no heartbeat was received duration the serverHealthinessTimeout, send the
	// timeout error to reconnectErrorChan
	timer := time.NewTimer(hc.healthinessTimout)
	defer timer.Stop()

	for {
		select {
		case heartbeat := <-hc.heartbeatChan:
			logger.V(4).Info("heartbeat received", "heartbeat", heartbeat)

			// reset timer safely
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(hc.healthinessTimout)
		case <-timer.C:
			select {
			case hc.errChan <- fmt.Errorf("stream timeout: no heartbeat received for %v", hc.healthinessTimout):
			default:
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

func (hc *HealthChecker) bypass(ctx context.Context) {
	logger := klog.FromContext(ctx)
	for {
		select {
		case msg := <-hc.heartbeatChan:
			logger.V(4).Info("heartbeat received", "heartbeat", msg)
		case <-ctx.Done():
			return
		}
	}
}
