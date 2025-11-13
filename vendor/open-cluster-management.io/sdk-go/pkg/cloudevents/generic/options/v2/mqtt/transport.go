package mqtt

import (
	"context"
	"fmt"
	"sync"

	cloudeventsmqtt "github.com/cloudevents/sdk-go/protocol/mqtt_paho/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/eclipse/paho.golang/paho"
	"k8s.io/klog/v2"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
)

type pubTopicGetter func(context.Context, cloudevents.Event) (string, error)
type subscribeGetter func() (*paho.Subscribe, error)

type mqttTransport struct {
	opts *mqtt.MQTTOptions

	mu         sync.RWMutex
	subscribed bool
	closeChan  chan struct{}
	errorChan  chan error
	msgChan    chan *paho.Publish

	clientID string

	getPublishTopic pubTopicGetter
	getSubscribe    subscribeGetter

	client *paho.Client
}

func newTransport(clientID string, opts *mqtt.MQTTOptions, pubTopicGetter pubTopicGetter, subscribeGetter subscribeGetter) *mqttTransport {
	return &mqttTransport{
		opts:            opts,
		clientID:        clientID,
		errorChan:       make(chan error, 1),
		getPublishTopic: pubTopicGetter,
		getSubscribe:    subscribeGetter,
	}
}

func (t *mqttTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	logger := klog.FromContext(ctx)
	tcpConn, err := t.opts.Dialer.Dial()
	if err != nil {
		return err
	}

	config := paho.ClientConfig{
		ClientID: t.clientID,
		Conn:     tcpConn,
		OnClientError: func(err error) {
			select {
			case t.errorChan <- err:
			default:
				logger.Error(err, "mqtt client error")
			}
		},
	}

	t.client = paho.NewClient(config)
	t.client.SetDebugLogger(mqtt.NewPahoDebugLogger(logger))
	t.client.SetErrorLogger(mqtt.NewPahoErrorLogger(logger))

	connAck, err := t.client.Connect(ctx, t.opts.GetMQTTConnectOption(t.clientID))
	if err != nil {
		return err
	}
	if connAck.ReasonCode != 0 {
		return fmt.Errorf("failed to establish the connection: %s", connAck.String())
	}

	// Initialize closeChan and msgChan to support reconnect cycles
	t.closeChan = make(chan struct{})
	// TODO consider to make the channel size configurable
	t.msgChan = make(chan *paho.Publish, 100)

	logger.Info("mqtt is connected", "brokerHost", t.opts.Dialer.BrokerHost)

	return nil
}

func (t *mqttTransport) Send(ctx context.Context, evt cloudevents.Event) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.client == nil {
		return fmt.Errorf("transport not connected")
	}

	topic, err := t.getPublishTopic(ctx, evt)
	if err != nil {
		return err
	}

	msg := &paho.Publish{
		QoS:   byte(t.opts.PubQoS),
		Topic: topic,
	}

	if err := cloudeventsmqtt.WritePubMessage(ctx, (*binding.EventMessage)(&evt), msg); err != nil {
		return err
	}

	if _, err := t.client.Publish(ctx, msg); err != nil {
		return err
	}

	return nil
}

func (t *mqttTransport) Subscribe(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.client == nil {
		return fmt.Errorf("transport not connected")
	}

	if t.subscribed {
		return fmt.Errorf("transport has already subscribed")
	}

	subscribe, err := t.getSubscribe()
	if err != nil {
		return err
	}

	t.client.AddOnPublishReceived(func(m paho.PublishReceived) (bool, error) {
		select {
		case t.msgChan <- m.Packet:
			return true, nil
		case <-t.getCloseChan():
			// Only drop messages if we're shutting down
			return false, fmt.Errorf("transport closed")
		}
		// No default case - will block until channel has space (no message loss)
	})

	if _, err := t.client.Subscribe(ctx, subscribe); err != nil {
		return err
	}

	t.subscribed = true

	logger := klog.FromContext(ctx)
	for _, sub := range subscribe.Subscriptions {
		logger.Info("subscribed to mqtt broker", "topic", sub.Topic, "QoS", sub.QoS)
	}

	return nil
}

// Receive starts receiving events and invokes the provided handler for each event.
// This is a BLOCKING call that runs an event loop until the context is cancelled.
func (t *mqttTransport) Receive(ctx context.Context, handleFn options.ReceiveHandlerFn) error {
	t.mu.RLock()
	if !t.subscribed {
		t.mu.RUnlock()
		return fmt.Errorf("transport not subscribed")
	}
	t.mu.RUnlock()

	logger := klog.FromContext(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.getCloseChan():
			return nil
		case m, ok := <-t.msgChan:
			if !ok {
				return nil
			}
			evt, err := binding.ToEvent(ctx, cloudeventsmqtt.NewMessage(m))
			if err != nil {
				logger.Error(err, "invalid event")
				continue
			}

			handleFn(ctx, *evt)
		}
	}
}

func (t *mqttTransport) ErrorChan() <-chan error {
	return t.errorChan
}

func (t *mqttTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	klog.FromContext(ctx).Info("close mqtt transport")

	if t.client == nil {
		// no client, do nothing
		return nil
	}

	// Guard against double-close panic - check if already closed
	if t.closeChan != nil {
		select {
		case <-t.closeChan:
			// already closed
		default:
			close(t.closeChan)
		}
	}

	t.subscribed = false

	return t.client.Disconnect(&paho.Disconnect{ReasonCode: 0})
}

func (t *mqttTransport) getCloseChan() chan struct{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.closeChan
}
