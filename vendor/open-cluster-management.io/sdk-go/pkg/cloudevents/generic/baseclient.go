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
)

const (
	restartReceiverSignal = iota
	stopReceiverSignal
)

type receiveFn func(ctx context.Context, evt cloudevents.Event)

type baseClient struct {
	sync.RWMutex
	cloudEventsOptions     options.CloudEventsOptions
	cloudEventsProtocol    options.CloudEventsProtocol
	cloudEventsClient      cloudevents.Client
	cloudEventsRateLimiter flowcontrol.RateLimiter
	receiverChan           chan int
	reconnectedChan        chan struct{}
}

func (c *baseClient) connect(ctx context.Context) error {
	var err error
	c.cloudEventsClient, err = c.newCloudEventsClient(ctx)
	if err != nil {
		return err
	}

	// start a go routine to handle cloudevents client connection errors
	go func() {
		var err error

		// the reconnect backoff will stop at [1,5) min interval. If we don't backoff for 10min, we reset the backoff.
		delayFn := wait.Backoff{
			Duration: 5 * time.Second,
			Cap:      1 * time.Minute,
			Steps:    12, // now a required argument
			Factor:   5.0,
			Jitter:   1.0,
		}.DelayWithReset(&clock.RealClock{}, 10*time.Minute)
		cloudEventsClient := c.cloudEventsClient

		for {
			if cloudEventsClient == nil {
				klog.V(4).Infof("reconnecting the cloudevents client")

				c.cloudEventsClient, err = c.newCloudEventsClient(ctx)
				// TODO enhance the cloudevents SKD to avoid wrapping the error type to distinguish the net connection
				// errors
				if err != nil {
					// failed to reconnect, try agin
					runtime.HandleError(fmt.Errorf("the cloudevents client reconnect failed, %v", err))
					<-wait.RealTimer(delayFn()).C()
					continue
				}

				// the cloudevents network connection is back, refresh the current cloudevents client and send the
				// receiver restart signal
				klog.V(4).Infof("the cloudevents client is reconnected")
				c.resetClient(cloudEventsClient)
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

				// the cloudevents client network connection is closed, send the receiver stop signal, set the current
				// client to nil and retry
				c.sendReceiverSignal(stopReceiverSignal)

				err = c.cloudEventsProtocol.Close(ctx)
				if err != nil {
					runtime.HandleError(fmt.Errorf("failed to close the cloudevents protocol, %v", err))
				}
				cloudEventsClient = nil

				c.resetClient(cloudEventsClient)

				<-wait.RealTimer(delayFn()).C()
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
		klog.Warningf(fmt.Sprintf("Waited for %v due to client-side throttling, not priority and fairness, request: %s",
			latency, evt))
	}

	sendingCtx, err := c.cloudEventsOptions.WithContext(ctx, evt.Context)
	if err != nil {
		return err
	}

	// make sure the current client is the newest
	c.RLock()
	defer c.RUnlock()

	if c.cloudEventsClient == nil {
		return fmt.Errorf("the cloudevents client is not ready")
	}

	klog.V(4).Infof("Sending event: %v\n%s", sendingCtx, evt)
	if result := c.cloudEventsClient.Send(sendingCtx, evt); cloudevents.IsUndelivered(result) {
		return fmt.Errorf("failed to send event %s, %v", evt, result)
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
		cloudEventsClient := c.cloudEventsClient

		for {
			if cloudEventsClient != nil {
				go func() {
					if err := cloudEventsClient.StartReceiver(receiverCtx, func(evt cloudevents.Event) {
						klog.V(4).Infof("Received event: %s", evt)
						receive(receiverCtx, evt)
					}); err != nil {
						runtime.HandleError(fmt.Errorf("failed to receive cloudevents, %v", err))
					}
				}()
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
					// make sure the current client is the newest
					c.RLock()
					cloudEventsClient = c.cloudEventsClient
					c.RUnlock()

					// rebuild the receiver context
					receiverCtx, receiverCancel = context.WithCancel(context.TODO())
				case stopReceiverSignal:
					klog.V(4).Infof("stop the cloudevents receiver")
					receiverCancel()
					cloudEventsClient = nil
				default:
					runtime.HandleError(fmt.Errorf("unknown receiver signal %d", signal))
				}
			}
		}
	}()
}

func (c *baseClient) resetClient(client cloudevents.Client) {
	c.Lock()
	defer c.Unlock()

	c.cloudEventsClient = client
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

func (c *baseClient) newCloudEventsClient(ctx context.Context) (cloudevents.Client, error) {
	var err error
	c.cloudEventsProtocol, err = c.cloudEventsOptions.Protocol(ctx)
	if err != nil {
		return nil, err
	}
	c.cloudEventsClient, err = cloudevents.NewClient(c.cloudEventsProtocol)
	if err != nil {
		return nil, err
	}
	return c.cloudEventsClient, nil
}
