package protocol

import (
	"context"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc"

	"github.com/cloudevents/sdk-go/v2/binding"
	cecontext "github.com/cloudevents/sdk-go/v2/context"
	"github.com/cloudevents/sdk-go/v2/protocol"

	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
)

// protocol for grpc
// define protocol for grpc

type Protocol struct {
	client          pbv1.CloudEventServiceClient
	subscribeOption *SubscribeOption
	// receiver
	incoming chan *pbv1.CloudEvent
	// inOpen
	openerMutex sync.Mutex

	closeChan chan struct{}
}

var (
	_ protocol.Sender   = (*Protocol)(nil)
	_ protocol.Opener   = (*Protocol)(nil)
	_ protocol.Receiver = (*Protocol)(nil)
	_ protocol.Closer   = (*Protocol)(nil)
)

// new create grpc protocol
func NewProtocol(clientConn grpc.ClientConnInterface, opts ...Option) (*Protocol, error) {
	if clientConn == nil {
		return nil, fmt.Errorf("the client connection must not be nil")
	}

	// TODO: support clientID and error handling in grpc connection
	p := &Protocol{
		client: pbv1.NewCloudEventServiceClient(clientConn),
		// subClient:
		incoming:  make(chan *pbv1.CloudEvent),
		closeChan: make(chan struct{}),
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

	logger := cecontext.LoggerFrom(ctx)
	logger.Infof("publishing event with id: %v", msg.Id)
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

	logger := cecontext.LoggerFrom(ctx)
	subClient, err := p.client.Subscribe(ctx, &pbv1.SubscriptionRequest{
		Source:      p.subscribeOption.Source,
		ClusterName: p.subscribeOption.ClusterName,
		DataType:    p.subscribeOption.DataType,
	})
	if err != nil {
		return err
	}

	if p.subscribeOption.Source != "" {
		logger.Infof("subscribing events for: %v with data types: %v", p.subscribeOption.Source, p.subscribeOption.DataType)
	} else {
		logger.Infof("subscribing events for cluster: %v with data types: %v", p.subscribeOption.ClusterName, p.subscribeOption.DataType)
	}

	go func() {
		for {
			msg, err := subClient.Recv()
			if err != nil {
				return
			}
			p.incoming <- msg
		}
	}()

	// Wait until external or internal context done
	select {
	case <-ctx.Done():
	case <-p.closeChan:
	}

	return nil
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
