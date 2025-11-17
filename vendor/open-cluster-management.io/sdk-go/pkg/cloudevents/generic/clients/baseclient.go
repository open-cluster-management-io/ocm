package clients

import (
	"context"
	"fmt"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/metrics"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/utils"
)

const (
	restartReceiverSignal = iota
	stopReceiverSignal
)

// the reconnect backoff will stop at [1,5) min interval. If we don't backoff for 10min, we reset the backoff.
var DelayFn = wait.Backoff{
	Duration: 5 * time.Second,
	Cap:      1 * time.Minute,
	Steps:    12, // now a required argument
	Factor:   5.0,
	Jitter:   1.0,
}.DelayWithReset(&clock.RealClock{}, 10*time.Minute)

// receiveFn is an internal callback function for processing received CloudEvents with context.
type receiveFn func(ctx context.Context, evt cloudevents.Event)

// baseClient provides the core functionality for CloudEvents source and agent clients.
//
// It handles three primary responsibilities:
// 1. Automatic reconnection when the transport connection fails
// 2. Event subscription management with receiver restart capability
// 3. Rate-limited event publishing
//
// The client maintains connection state and automatically attempts to reconnect when
// errors are detected from the transport layer. Upon successful reconnection, it restarts
// the event receiver and notifies listeners via the reconnectedChan.
//
// Thread-safety: All public methods are safe for concurrent use. The clientReady flag
// and receiverChan are protected by an embedded RWMutex.
type baseClient struct {
	sync.RWMutex
	clientID               string // the client id is used to identify the client, either a source or an agent ID
	transport              options.CloudEventTransport
	cloudEventsRateLimiter flowcontrol.RateLimiter
	receiverChan           chan int
	reconnectedChan        chan struct{}
	clientReady            bool
}

func (c *baseClient) connect(ctx context.Context) error {
	logger := klog.FromContext(ctx)

	var err error
	err = c.transport.Connect(ctx)
	if err != nil {
		return err
	}
	c.setClientReady(true)

	// start a go routine to handle cloudevents client connection errors
	go func() {
		for {
			if !c.isClientReady() {
				logger.V(2).Info("reconnecting the cloudevents client")

				err = c.transport.Connect(ctx)
				// TODO enhance the cloudevents SKD to avoid wrapping the error type to distinguish the net connection
				// errors
				if err != nil {
					// failed to reconnect, try agin
					runtime.HandleErrorWithContext(ctx, err, "the cloudevents client reconnect failed")
					<-wait.RealTimer(DelayFn()).C()
					continue
				}
				// the cloudevents network connection is back, mark the client ready and send the receiver restart signal
				logger.V(2).Info("the cloudevents client is reconnected")
				metrics.IncreaseClientReconnectedCounter(c.clientID)
				c.setClientReady(true)
				c.sendReceiverSignal(restartReceiverSignal)
				c.sendReconnectedSignal()
			}

			select {
			case <-ctx.Done():
				if c.receiverChan != nil {
					close(c.receiverChan)
				}
				return
			case err, ok := <-c.transport.ErrorChan():
				if !ok {
					// error channel is closed, do nothing
					return
				}
				runtime.HandleErrorWithContext(ctx, err, "the cloudevents client is disconnected")

				// the cloudevents client network connection is closed, send the receiver stop signal, set the current client not ready
				// and close the current client
				c.sendReceiverSignal(stopReceiverSignal)
				c.setClientReady(false)
				if err := c.transport.Close(ctx); err != nil {
					runtime.HandleErrorWithContext(ctx, err, "failed to close the cloudevents protocol")
				}

				<-wait.RealTimer(DelayFn()).C()
			}
		}
	}()

	return nil
}

func (c *baseClient) publish(ctx context.Context, evt cloudevents.Event) error {
	logger := klog.FromContext(ctx)
	now := time.Now()

	if err := c.cloudEventsRateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("client rate limiter Wait returned an error: %w", err)
	}

	latency := time.Since(now)
	if latency > utils.LongThrottleLatency {
		logger.V(3).Info(
			"Client-side throttling delay (not priority and fairness)",
			"latency", latency,
			"request", evt.Context.GetID(),
		)
	}

	if !c.isClientReady() {
		return fmt.Errorf("the cloudevents client is not ready")
	}

	logger.V(2).Info("Sending event", "event", evt.Context)
	logger.V(5).Info("Sending event", "event", func() any { return evt.String() })
	if err := c.transport.Send(ctx, evt); err != nil {
		return err
	}

	return nil
}

func (c *baseClient) subscribe(ctx context.Context, receive receiveFn) {
	c.Lock()
	defer c.Unlock()

	logger := klog.FromContext(ctx)
	// make sure there is only one subscription go routine starting for one client.
	if c.receiverChan != nil {
		logger.V(2).Info("the subscription has already started")
		return
	}

	c.receiverChan = make(chan int)

	// start a go routine to handle cloudevents subscription
	go func() {
		receiverCtx, receiverCancel := context.WithCancel(context.TODO())
		startReceiving := true

		for {
			if startReceiving {
				go func() {
					if err := c.transport.Receive(receiverCtx, func(evt cloudevents.Event) {
						logger.V(2).Info("Received event", "event", evt.Context)
						logger.V(5).Info("Received event", "event", func() any { return evt.String() })

						receive(receiverCtx, evt)
					}); err != nil {
						runtime.HandleError(fmt.Errorf("failed to receive cloudevents, %v", err))
					}
				}()
				startReceiving = false
			}

			select {
			case <-ctx.Done():
				receiverCancel()
				return
			case signal, ok := <-c.receiverChan:
				if !ok {
					// receiver channel is closed, stop the receiver
					receiverCancel()
					return
				}

				switch signal {
				case restartReceiverSignal:
					logger.V(2).Info("restart the cloudevents receiver")
					// rebuild the receiver context and restart receiving
					receiverCtx, receiverCancel = context.WithCancel(context.TODO())
					startReceiving = true
				case stopReceiverSignal:
					logger.V(2).Info("stop the cloudevents receiver")
					receiverCancel()
				default:
					runtime.HandleErrorWithContext(ctx, fmt.Errorf("unknown receiver signal"), "", "signal", signal)
				}
			}
		}
	}()
}

func (c *baseClient) sendReceiverSignal(signal int) {
	c.RLock()
	defer c.RUnlock()

	if c.receiverChan != nil {
		c.receiverChan <- signal
	}
}

func (c *baseClient) sendReconnectedSignal() {
	c.RLock()
	defer c.RUnlock()
	c.reconnectedChan <- struct{}{}
}

func (c *baseClient) isClientReady() bool {
	c.RLock()
	defer c.RUnlock()
	return c.clientReady
}

func (c *baseClient) setClientReady(ready bool) {
	c.Lock()
	defer c.Unlock()
	c.clientReady = ready
}
