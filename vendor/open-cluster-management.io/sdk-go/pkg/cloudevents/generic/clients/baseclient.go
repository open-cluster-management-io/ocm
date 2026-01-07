package clients

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"open-cluster-management.io/sdk-go/pkg/logging"

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
	// startReceiverSignal signals the receiver goroutine to start with a new context.
	// This is sent after successful connection and subscription to start event processing.
	startReceiverSignal = iota
	// stopReceiverSignal signals the receiver goroutine to cancel its context and stop processing events.
	// This is sent when the transport connection fails or encounters an error.
	stopReceiverSignal
)

// the reconnect backoff will stop at [5s, 1min) interval. If we don't backoff for 10min, we reset the backoff.
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
// the event receiver and notifies listeners via the resyncChan.
//
// Thread-safety: All public methods are safe for concurrent use. The connected and subscribed flags
// use atomic operations for thread-safe access.
type baseClient struct {
	clientID               string // the client id is used to identify the client, either a source or an agent ID
	transport              options.CloudEventTransport
	cloudEventsRateLimiter flowcontrol.RateLimiter
	receiverChan           chan int
	resyncChan             chan struct{}
	subscribeChan          chan struct{}
	connected              atomic.Bool
	subscribed             atomic.Bool
}

func newBaseClient(clientID string, transport options.CloudEventTransport, limit utils.EventRateLimit) *baseClient {
	return &baseClient{
		clientID:               clientID,
		transport:              transport,
		cloudEventsRateLimiter: utils.NewRateLimiter(limit),
		resyncChan:             make(chan struct{}, 1),
		subscribeChan:          make(chan struct{}, 1),
		receiverChan:           make(chan int, 2), // Allow both stop and start signals to be buffered
	}
}

func (c *baseClient) connect(ctx context.Context) error {
	logger := klog.FromContext(ctx)

	var err error
	err = c.transport.Connect(ctx)
	if err != nil {
		return err
	}
	c.connected.Store(true)

	// Start a goroutine to monitor transport health and handle reconnection.
	// This goroutine runs a loop that:
	// 1. Checks if disconnected and attempts to reconnect with exponential backoff
	// 2. Listens for errors from the transport's error channel
	// 3. On error: sends stopReceiverSignal, sets connected to false, closes the transport, and waits for backoff delay
	// 4. On successful reconnection: sets connected to true and sends signal to subscribeChan to trigger subscription
	go func() {
		for {
			if !c.connected.Load() {
				logger.V(2).Info("reconnecting the cloudevents client")

				err = c.transport.Connect(ctx)
				// TODO enhance the cloudevents SKD to avoid wrapping the error type to distinguish the net connection
				// errors
				if err != nil {
					// failed to reconnect, try again
					runtime.HandleErrorWithContext(ctx, err, "the cloudevents client reconnect failed")
					<-wait.RealTimer(DelayFn()).C()
					continue
				}
				// the cloudevents network connection is back, set connected to true and notify the client to resubscribe
				logger.V(2).Info("the cloudevents client is reconnected")
				metrics.IncreaseClientReconnectedCounter(c.clientID)
				c.connected.Store(true)
				select {
				case c.subscribeChan <- struct{}{}:
					// Signal sent successfully
				default:
					// Subscribe channel is unavailable, that's okay - don't block
					klog.FromContext(ctx).Info("subscribe signal not sent, subscribe channel is unavailable")
				}
			}

			select {
			case <-ctx.Done():
				// Don't need to close channels here. Closing channels here would race
				// with current goroutines trying to send/receive on the same channels.
				// The channels will be garbage collected once all goroutines exit.
				return
			case err, ok := <-c.transport.ErrorChan():
				if !ok {
					// error channel is closed, do nothing
					return
				}
				runtime.HandleErrorWithContext(ctx, err, "the cloudevents client is disconnected")

				// transport reported an error (connection failed), stop the receiver, set connected to false,
				// close the transport, and wait for backoff delay before attempting reconnection
				select {
				case c.receiverChan <- stopReceiverSignal:
					// Signal sent successfully
				default:
					// Receiver channel is unavailable, that's okay - don't block
					klog.FromContext(ctx).V(2).Info("stopReceiverSignal not sent, receiver channel unavailable")
				}
				c.connected.Store(false)
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
	logger := logging.SetLogTracingByCloudEvent(klog.FromContext(ctx), &evt)
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

	if !c.connected.Load() {
		return fmt.Errorf("the cloudevents client is not ready")
	}

	if logger.V(5).Enabled() {
		evtData, _ := evt.MarshalJSON()
		logger.V(5).Info("Sending event", "event", string(evtData))
	} else {
		logger.V(2).Info("Sending event",
			"eventType", evt.Type(), "extensions", evt.Extensions())
	}
	if err := c.transport.Send(ctx, evt); err != nil {
		return err
	}

	return nil
}

func (c *baseClient) subscribe(ctx context.Context, receive receiveFn) {
	logger := klog.FromContext(ctx)
	// make sure there is only one subscription go routine starting for one client.
	// Swap returns the old value, so if it was already true, we've already subscribed
	if c.subscribed.Swap(true) {
		logger.V(2).Info("the subscription has already started")
		return
	}

	// Start a goroutine to handle subscription.
	// This goroutine listens for signals on subscribeChan (sent on initial subscribe,
	// and after reconnection by the connection monitor goroutine), attempts to subscribe to the
	// event stream, and if successful, sends startReceiverSignal to start the receiver and
	// notifies any listeners via resyncChan that they should resync their resources.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.subscribeChan:
				if err := c.transport.Subscribe(ctx); err != nil {
					// Failed to send subscribe request, it should be connection failed, will retry on next reconnection
					runtime.HandleErrorWithContext(ctx, err, "failed to subscribe after connection")
					continue
				}

				// Send startReceiverSignal to start/restart the receiver after successful subscription.
				// The receiver lifecycle goroutine will create a new context and spawn a goroutine
				// to call c.transport.Receive() if not already running.
				select {
				case c.receiverChan <- startReceiverSignal:
					// Signal sent successfully
				default:
					// Receiver channel full or closed, that's okay - will retry on next reconnection
					klog.FromContext(ctx).V(2).Info("startReceiverSignal not sent, receiver channel unavailable")
				}

				// Notify the client caller to resync the resources
				select {
				case c.resyncChan <- struct{}{}:
					// Signal sent successfully
				default:
					// Resync channel is unavailable, that's okay - don't block
					klog.FromContext(ctx).V(2).Info("resync signal not sent, resync channel unavailable")
				}
			}
		}
	}()

	// Start a goroutine to manage the event receiver lifecycle.
	// This goroutine responds to signals on receiverChan:
	// - startReceiverSignal: Creates a new receiver context and spawns a c.transport.Receive() goroutine
	//   if not already running (tracked by startReceiving flag)
	// - stopReceiverSignal: Cancels the receiver context to stop the running c.transport.Receive()
	//   goroutine and clears the startReceiving flag
	go func() {
		var startReceiving bool
		var receiverCtx context.Context
		var receiverCancel context.CancelFunc

		for {
			select {
			case <-ctx.Done():
				if receiverCancel != nil {
					receiverCancel()
				}
				return
			case signal, ok := <-c.receiverChan:
				if !ok {
					// receiver channel is closed, stop the receiver
					if receiverCancel != nil {
						receiverCancel()
					}
					return
				}

				switch signal {
				case startReceiverSignal:
					if !startReceiving {
						logger.V(2).Info("start the cloudevents receiver")
						// Cancel old context before creating a new one to prevent context leak
						if receiverCancel != nil {
							receiverCancel()
						}

						// create a new receiver context
						receiverCtx, receiverCancel = context.WithCancel(ctx)
						// Set flag before spawning goroutine to prevent race condition
						startReceiving = true
						go func() {
							if err := c.transport.Receive(receiverCtx, func(handlerCtx context.Context, evt cloudevents.Event) {
								receiveLogger := logging.SetLogTracingByCloudEvent(klog.FromContext(handlerCtx), &evt)
								handlerCtx = klog.NewContext(handlerCtx, receiveLogger)
								if receiveLogger.V(5).Enabled() {
									evtData, _ := evt.MarshalJSON()
									receiveLogger.V(5).Info("Received event", "event", string(evtData))
								} else {
									receiveLogger.V(2).Info("Received event",
										"eventType", evt.Type(), "extensions", evt.Extensions())
								}
								receive(handlerCtx, evt)
							}); err != nil {
								runtime.HandleErrorWithContext(ctx, err, "failed to receive cloudevents")
							}
						}()
					}
				case stopReceiverSignal:
					logger.V(2).Info("stop the cloudevents receiver")
					if receiverCancel != nil {
						receiverCancel()
						receiverCancel = nil
						receiverCtx = nil
					}
					startReceiving = false
				default:
					runtime.HandleErrorWithContext(ctx, fmt.Errorf("unknown receiver signal"), "", "signal", signal)
				}
			}
		}
	}()

	// Send initial subscription signal to trigger the first subscription attempt
	select {
	case c.subscribeChan <- struct{}{}:
		// Signal sent successfully
	default:
		// Channel full or closed - should not happen during normal initialization
		logger.V(2).Info("initial subscribe signal not sent, channel unavailable")
	}
}
