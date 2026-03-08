package protocol

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/heartbeat"

	"google.golang.org/grpc"

	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/cloudevents/sdk-go/v2/protocol"

	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// protocol for grpc
// define protocol for grpc

type Protocol struct {
	clientConn      *grpc.ClientConn
	client          pbv1.CloudEventServiceClient
	subscribeOption *SubscribeOption
	// receiver
	incoming chan *pbv1.CloudEvent
	// inOpen
	openerMutex sync.Mutex

	closeChan chan struct{}

	// reconnectErrorChan is to send an error message to reconnect the connection
	reconnectErrorChan chan error

	// serverHealthinessTimeout is the max duration that client will reconnect if no server healthiness
	// status is received in this duration.
	serverHealthinessTimeout *time.Duration
}

var (
	_ protocol.Sender   = (*Protocol)(nil)
	_ protocol.Opener   = (*Protocol)(nil)
	_ protocol.Receiver = (*Protocol)(nil)
	_ protocol.Closer   = (*Protocol)(nil)
)

// new create grpc protocol
func NewProtocol(clientConn *grpc.ClientConn, opts ...Option) (*Protocol, error) {
	if clientConn == nil {
		return nil, fmt.Errorf("the client connection must not be nil")
	}

	// TODO: support clientID and error handling in grpc connection
	p := &Protocol{
		clientConn: clientConn,
		client:     pbv1.NewCloudEventServiceClient(clientConn),
		incoming:   make(chan *pbv1.CloudEvent),
		closeChan:  make(chan struct{}),
	}

	if err := p.applyOptions(opts...); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Protocol) applyOptions(opts ...Option) error {
	for _, fn := range opts {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}

func (p *Protocol) Send(ctx context.Context, m binding.Message, transformers ...binding.Transformer) error {
	var err error
	defer func() {
		err = m.Finish(err)
	}()

	msg := &pbv1.CloudEvent{}
	err = WritePBMessage(ctx, m, msg, transformers...)
	if err != nil {
		return err
	}

	logger := klog.FromContext(ctx)
	logger.Info("publishing event", "messageID", msg.Id)
	_, err = p.client.Publish(ctx, &pbv1.PublishRequest{
		Event: msg,
	})
	if err != nil {
		return err
	}
	return err
}

func (p *Protocol) OpenInbound(ctx context.Context) error {
	if p.subscribeOption == nil {
		return fmt.Errorf("the subscribe option must not be nil")
	}

	if len(p.subscribeOption.Source) == 0 && len(p.subscribeOption.ClusterName) == 0 {
		return fmt.Errorf("the source and cluster name of subscribe option cannot both be empty")
	}

	p.openerMutex.Lock()
	defer p.openerMutex.Unlock()

	logger := klog.FromContext(ctx)
	subClient, err := p.client.Subscribe(ctx, &pbv1.SubscriptionRequest{
		Source:      p.subscribeOption.Source,
		ClusterName: p.subscribeOption.ClusterName,
		DataType:    p.subscribeOption.DataType,
	})
	if err != nil {
		return err
	}

	if p.subscribeOption.Source != "" {
		logger.Info("subscribing events for source", "source", p.subscribeOption.Source, "eventDataType", p.subscribeOption.DataType)
	} else {
		logger.Info("subscribing events for cluster", "cluster", p.subscribeOption.ClusterName, "eventDataType", p.subscribeOption.DataType)
	}

	subCtx, cancel := context.WithCancel(ctx)
	healthChecker := heartbeat.NewHealthChecker(p.serverHealthinessTimeout, p.reconnectErrorChan)

	// start to receive the events from stream
	go p.startEventsReceiver(subCtx, subClient, healthChecker.Input())

	// start to watch the stream heartbeat
	go healthChecker.Start(subCtx)

	// Wait until external or internal context done
	select {
	case <-subCtx.Done():
	case <-p.closeChan:
	}

	// ensure the event receiver and heartbeat watcher are done
	cancel()

	logger.Info("Close grpc client connection")
	return p.clientConn.Close()
}

// Receive implements Receiver.Receive
func (p *Protocol) Receive(ctx context.Context) (binding.Message, error) {
	select {
	case m, ok := <-p.incoming:
		if !ok {
			return nil, io.EOF
		}
		msg := NewMessage(m)
		return msg, nil
	case <-ctx.Done():
		return nil, io.EOF
	}
}

func (p *Protocol) Close(ctx context.Context) error {
	close(p.closeChan)
	return nil
}

func (p *Protocol) startEventsReceiver(ctx context.Context,
	subClient pbv1.CloudEventService_SubscribeClient, heartbeatCh chan *pbv1.CloudEvent) {
	for {
		evt, err := subClient.Recv()
		if err != nil {
			select {
			case p.reconnectErrorChan <- fmt.Errorf("subscribe stream failed: %w", err):
			default:
			}
			return
		}

		if evt.Type == types.HeartbeatCloudEventsType {
			select {
			case heartbeatCh <- evt:
			case <-ctx.Done():
				return
			}
			continue
		}

		select {
		case p.incoming <- evt:
		case <-ctx.Done():
			return
		}
	}
}
