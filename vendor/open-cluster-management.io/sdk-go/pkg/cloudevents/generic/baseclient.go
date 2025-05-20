package generic

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

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
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

type receiveFn func(ctx context.Context, evt cloudevents.Event)

type baseClient struct {
	sync.RWMutex
	clientID               string // the client id is used to identify the client, either a source or an agent ID
	cloudEventsOptions     options.CloudEventsOptions
	cloudEventsProtocol    options.CloudEventsProtocol
	cloudEventsClient      cloudevents.Client
	cloudEventsRateLimiter flowcontrol.RateLimiter
	receiverChan           chan int
	reconnectedChan        chan struct{}
	clientReady            bool
	dataType               types.CloudEventsDataType
}

func (c *baseClient) connect(ctx context.Context) error {
	var err error
	c.cloudEventsClient, err = c.newCloudEventsClient(ctx)
	if err != nil {
		return err
	}

	// start a go routine to handle cloudevents client connection errors
	go func() {
		for {
			if !c.isClientReady() {
				klog.V(4).Infof("reconnecting the cloudevents client")

				c.cloudEventsClient, err = c.newCloudEventsClient(ctx)
				// TODO enhance the cloudevents SKD to avoid wrapping the error type to distinguish the net connection
				// errors
				if err != nil {
					// failed to reconnect, try agin
					runtime.HandleError(fmt.Errorf("the cloudevents client reconnect failed, %v", err))
					<-wait.RealTimer(DelayFn()).C()
					continue
				}
				// the cloudevents network connection is back, mark the client ready and send the receiver restart signal
				klog.V(4).Infof("the cloudevents client is reconnected")
				increaseClientReconnectedCounter(c.clientID)
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
			case err, ok := <-c.cloudEventsOptions.ErrorChan():
				if !ok {
					// error channel is closed, do nothing
					return
				}

				runtime.HandleError(fmt.Errorf("the cloudevents client is disconnected, %v", err))

				// the cloudevents client network connection is closed, send the receiver stop signal, set the current client not ready
				// and close the current client
				c.sendReceiverSignal(stopReceiverSignal)
				c.setClientReady(false)
				if err := c.cloudEventsProtocol.Close(ctx); err != nil {
					runtime.HandleError(fmt.Errorf("failed to close the cloudevents protocol, %v", err))
				}

				<-wait.RealTimer(DelayFn()).C()
			}
		}
	}()

	return nil
}

func (c *baseClient) publish(ctx context.Context, evt cloudevents.Event) error {
	now := time.Now()

	if err := c.cloudEventsRateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("client rate limiter Wait returned an error: %w", err)
	}

	latency := time.Since(now)
	if latency > longThrottleLatency {
		klog.Warningf("Waited for %v due to client-side throttling, not priority and fairness, request: %s",
			latency, evt.Context)
	}

	sendingCtx, err := c.cloudEventsOptions.WithContext(ctx, evt.Context)
	if err != nil {
		return err
	}

	if !c.isClientReady() {
		return fmt.Errorf("the cloudevents client is not ready")
	}

	klog.V(4).Infof("Sending event: %v\n%s", sendingCtx, evt.Context)
	klog.V(5).Infof("Sending event: evt=%s", evt)
	if err := c.cloudEventsClient.Send(sendingCtx, evt); cloudevents.IsUndelivered(err) {
		return err
	}

	return nil
}

func (c *baseClient) subscribe(ctx context.Context, receive receiveFn) {
	c.Lock()
	defer c.Unlock()

	// make sure there is only one subscription go routine starting for one client.
	if c.receiverChan != nil {
		klog.Warningf("the subscription has already started")
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
					if err := c.cloudEventsClient.StartReceiver(receiverCtx, func(evt cloudevents.Event) {
						klog.V(4).Infof("Received event: %s", evt.Context)
						klog.V(5).Infof("Received event: evt=%s", evt)

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
					klog.V(4).Infof("restart the cloudevents receiver")
					// rebuild the receiver context and restart receiving
					receiverCtx, receiverCancel = context.WithCancel(context.TODO())
					startReceiving = true
				case stopReceiverSignal:
					klog.V(4).Infof("stop the cloudevents receiver")
					receiverCancel()
				default:
					runtime.HandleError(fmt.Errorf("unknown receiver signal %d", signal))
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

func (c *baseClient) newCloudEventsClient(ctx context.Context) (cloudevents.Client, error) {
	var err error
	c.cloudEventsProtocol, err = c.cloudEventsOptions.Protocol(ctx, c.dataType)
	if err != nil {
		return nil, err
	}

	cloudEventsClient, err := cloudevents.NewClient(c.cloudEventsProtocol)
	if err != nil {
		return nil, err
	}

	c.setClientReady(true)

	return cloudEventsClient, nil
}
