package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"

	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/constants"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/heartbeat"
)

type subscriptionRequestGetter func() *pbv1.SubscriptionRequest

type grpcTransport struct {
	opts *grpcoptions.GRPCOptions

	client    pbv1.CloudEventServiceClient
	subClient pbv1.CloudEventService_SubscribeClient

	subID      string
	subscribed bool

	mu        sync.RWMutex
	closeChan chan struct{}
	errorChan chan error

	getSubscriptionRequest subscriptionRequestGetter
}

func newTransport(grpcOptions *grpcoptions.GRPCOptions, getter subscriptionRequestGetter) *grpcTransport {
	return &grpcTransport{
		opts:                   grpcOptions,
		errorChan:              make(chan error),
		getSubscriptionRequest: getter,
	}
}

func (t *grpcTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	logger := klog.FromContext(ctx)
	conn, err := t.opts.Dialer.Dial()
	if err != nil {
		return err
	}

	t.client = pbv1.NewCloudEventServiceClient(conn)

	// Initialize closeChan to support reconnect cycles
	t.closeChan = make(chan struct{})

	// Start a goroutine to monitor the gRPC connection state changes
	go t.monitorConnectionState(ctx, conn)

	logger.Info("grpc is connected", "grpcURL", t.opts.Dialer.URL)

	return nil
}

func (t *grpcTransport) Send(ctx context.Context, evt cloudevents.Event) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.client == nil {
		return fmt.Errorf("transport not connected")
	}

	pbEvt := &pbv1.CloudEvent{}
	if err := protocol.WritePBMessage(ctx, (*binding.EventMessage)(&evt), pbEvt); err != nil {
		return err
	}

	if _, err := t.client.Publish(ctx, &pbv1.PublishRequest{Event: pbEvt}); err != nil {
		return err
	}

	return nil
}

func (t *grpcTransport) Subscribe(ctx context.Context) error {
	logger := klog.FromContext(ctx)
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.client == nil {
		return fmt.Errorf("transport not connected")
	}

	if t.subscribed {
		return fmt.Errorf("transport has already subscribed")
	}

	subOption := t.getSubscriptionRequest()
	subClient, err := t.client.Subscribe(ctx, subOption)
	if err != nil {
		return err
	}

	// Wait for server to send subscription-id
	header, err := subClient.Header()
	if err != nil {
		return err
	}

	values := header.Get(constants.GRPCSubscriptionIDKey)
	if len(values) != 1 {
		// Header() succeeded but no subscription-id was sent (header is nil or empty).
		// This typically means the server rejected the subscription before sending headers
		// (e.g., authorization failure). The actual error is only available via Recv().
		// Call Recv() to get the real error from the server.
		recvErrCh := make(chan error, 1)
		go func() {
			_, err := subClient.Recv()
			recvErrCh <- err
		}()
		select {
		case recvErr := <-recvErrCh:
			if recvErr != nil {
				return recvErr
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			_ = subClient.CloseSend()
			return fmt.Errorf("no subscription-id in header (%v): recv timeout", header)
		}
		// If Recv() didn't return an error, this is a server-side configuration issue
		return fmt.Errorf("no subscription-id in header (%v)", header)
	}
	t.subID = values[0]
	t.subClient = subClient
	t.subscribed = true

	logger.Info("subscribed to grpc server", "subID", t.subID, "subOption", subOption)
	return nil
}

// Receive starts receiving events and invokes the provided handler for each event.
// This is a BLOCKING call that runs an event loop until the context is cancelled.
func (t *grpcTransport) Receive(ctx context.Context, handleFn options.ReceiveHandlerFn) error {
	t.mu.Lock()
	if t.subClient == nil {
		t.mu.Unlock()
		return fmt.Errorf("transport not subscribed")
	}
	t.mu.Unlock()

	logger := klog.FromContext(ctx).WithValues("subID", t.subID)
	subOption := t.getSubscriptionRequest()
	subCtx, cancel := context.WithCancel(ctx)
	subCtx = klog.NewContext(subCtx, logger)
	healthChecker := heartbeat.NewHealthChecker(t.opts.ServerHealthinessTimeout, t.errorChan)

	// start to receive the events from stream
	logger.Info("receiving events", "subOption", subOption)
	go t.startEventsReceiver(subCtx, healthChecker.Input(), handleFn)

	// start to watch the stream heartbeat
	go healthChecker.Start(subCtx)

	// wait until external or internal context done
	select {
	case <-ctx.Done():
	case <-t.getCloseChan():
	}

	// ensure the event receiver and heartbeat watcher are done
	cancel()

	logger.Info("stop receiving events", "subID", t.subID, "subOption", subOption)
	return nil
}

func (t *grpcTransport) ErrorChan() <-chan error {
	return t.errorChan
}

func (t *grpcTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	klog.FromContext(ctx).Info("close grpc transport")

	// Guard against double-close panic and nil channel
	if t.closeChan != nil {
		select {
		case <-t.closeChan:
			// Already closed
		default:
			close(t.closeChan)
		}
	}

	t.subscribed = false
	return t.opts.Dialer.Close()
}

func (t *grpcTransport) startEventsReceiver(ctx context.Context,
	heartbeatCh chan *pbv1.CloudEvent,
	handleFn options.ReceiveHandlerFn) {
	logger := klog.FromContext(ctx)
	for {
		pbEvt, err := t.subClient.Recv()
		if err != nil {
			st, ok := status.FromError(err)
			if ok && st.Code() == codes.Canceled {
				// canceled by transport, return directly.
				return
			}

			select {
			case t.errorChan <- fmt.Errorf("subscribe (subID=%s) stream failed: %w", t.subID, err):
			default:
				logger.Error(err, "subscribe stream failed", "subID", t.subID)
			}
			return
		}

		if pbEvt.Type == types.HeartbeatCloudEventsType {
			select {
			case heartbeatCh <- pbEvt:
			case <-ctx.Done():
				return
			}
			continue
		}

		evt, err := binding.ToEvent(ctx, protocol.NewMessage(pbEvt))
		if err != nil {
			logger.Error(err, "invalid event")
			continue
		}

		handleFn(ctx, *evt)

		select {
		case <-ctx.Done():
			return
		case <-t.getCloseChan():
			return
		default:
			// Continue receiving more events
		}
	}
}

func (t *grpcTransport) getCloseChan() chan struct{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.closeChan
}

// monitorConnectionState monitors the gRPC connection state changes and reports errors
// when the connection becomes disconnected.
func (t *grpcTransport) monitorConnectionState(ctx context.Context, conn *grpc.ClientConn) {
	logger := klog.FromContext(ctx)
	state := conn.GetState()
	for {
		if !conn.WaitForStateChange(ctx, state) {
			// the ctx is closed, stop this watch
			logger.Info("stop watching grpc connection state")
			return
		}

		newState := conn.GetState()
		// If any failure in any of the steps needed to establish connection, or any failure
		// encountered while expecting successful communication on established channel, the
		// grpc client connection state will be TransientFailure.
		// When client certificate is expired, client will proactively close the connection,
		// which will result in connection state changed from Ready to Shutdown.
		// When server is closed, client will NOT close or reestablish the connection proactively,
		// it will only change the connection state from Ready to Idle.
		if newState == connectivity.TransientFailure ||
			newState == connectivity.Shutdown ||
			newState == connectivity.Idle {
			select {
			case t.errorChan <- fmt.Errorf("grpc connection is disconnected (state=%s)", newState):
			default:
				logger.Info("no error channel available to report grpc disconnected", "state", newState)
			}

			if newState != connectivity.Shutdown {
				// don't close the connection if it's already shutdown
				if err := conn.Close(); err != nil {
					logger.Error(err, "failed to close grpc connection")
				}
			}

			return // exit the goroutine as the error handler function will handle the reconnection.
		}

		state = newState
	}
}
