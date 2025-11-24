package pubsub

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/pubsub/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var _ options.CloudEventTransport = &pubsubTransport{}

// pubsubTransport is a CloudEventTransport implementation for Pub/Sub.
type pubsubTransport struct {
	PubSubOptions
	// Source ID, required for source
	sourceID string
	// cluster name, required for agent
	clusterName string
	client      *pubsub.Client
	grpcConn    *grpc.ClientConn
	// Publisher for spec/status updates
	publisher *pubsub.Publisher
	// Publisher for resync broadcasts
	resyncPublisher *pubsub.Publisher
	// Subscriber for spec/status updates
	subscriber *pubsub.Subscriber
	// Subscriber for resync broadcasts
	resyncSubscriber *pubsub.Subscriber
	// errorChan is to send an error message to reconnect the connection
	errorChan chan error
}

func (o *pubsubTransport) Connect(ctx context.Context) error {
	clientOptions := []option.ClientOption{}
	if o.CredentialsFile != "" {
		clientOptions = append(clientOptions, option.WithCredentialsFile(o.CredentialsFile))
	}
	if o.Endpoint != "" {
		clientOptions = append(clientOptions, option.WithEndpoint(o.Endpoint))
		if strings.Contains(o.Endpoint, "localhost") || strings.Contains(o.Endpoint, "127.0.0.1") {
			// use the insecure connection for local development/testing
			pubsubConn, err := grpc.NewClient(o.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return err
			}
			o.grpcConn = pubsubConn
			clientOptions = append(clientOptions, option.WithGRPCConn(pubsubConn))
		}
	}

	if o.KeepaliveSettings != nil {
		// config keepalive parameters for pubsub client
		clientOptions = append(clientOptions, option.WithGRPCDialOption(grpc.WithKeepaliveParams(toGRPCKeepaliveParameter(o.KeepaliveSettings))))
	}

	client, err := pubsub.NewClient(ctx, o.ProjectID, clientOptions...)
	if err != nil {
		return err
	}

	// initialize pubsub client and publishers
	o.client = client
	if o.clusterName != "" && o.sourceID == "" {
		o.publisher = client.Publisher(o.Topics.AgentEvents)
		o.resyncPublisher = client.Publisher(o.Topics.AgentBroadcast)
	} else if o.sourceID != "" && o.clusterName == "" {
		o.publisher = client.Publisher(o.Topics.SourceEvents)
		o.resyncPublisher = client.Publisher(o.Topics.SourceBroadcast)
	} else {
		return fmt.Errorf("either source ID or cluster name must be set")
	}

	return nil
}

func (o *pubsubTransport) Subscribe(ctx context.Context) error {
	if o.client == nil {
		return fmt.Errorf("failed to initialize with nil pubsub client")
	}
	// initialize subscribers
	if o.clusterName != "" && o.sourceID == "" {
		o.subscriber = o.client.Subscriber(o.Subscriptions.SourceEvents)
		o.resyncSubscriber = o.client.Subscriber(o.Subscriptions.SourceBroadcast)
	} else if o.sourceID != "" && o.clusterName == "" {
		o.subscriber = o.client.Subscriber(o.Subscriptions.AgentEvents)
		o.resyncSubscriber = o.client.Subscriber(o.Subscriptions.AgentBroadcast)
	} else {
		return fmt.Errorf("either source ID or cluster name must be set")
	}

	// configure receive settings if provided
	if o.ReceiveSettings != nil {
		receiveSettings := toPubSubReceiveSettings(o.ReceiveSettings)
		o.subscriber.ReceiveSettings = receiveSettings
		o.resyncSubscriber.ReceiveSettings = receiveSettings
	}

	return nil
}

func (o *pubsubTransport) Send(ctx context.Context, evt cloudevents.Event) error {
	msg, err := Encode(evt)
	if err != nil {
		return err
	}

	eventType, err := types.ParseCloudEventsType(evt.Context.GetType())
	if err != nil {
		return fmt.Errorf("unsupported event type %s, %v", evt.Context.GetType(), err)
	}

	// determine publisher based on event type
	var result *pubsub.PublishResult
	if eventType.Action == types.ResyncRequestAction {
		result = o.resyncPublisher.Publish(ctx, msg)
	} else {
		result = o.publisher.Publish(ctx, msg)
	}

	// block until the result is returned
	_, err = result.Get(ctx)
	return err
}

func (o *pubsubTransport) Receive(ctx context.Context, fn options.ReceiveHandlerFn) error {
	errChan := make(chan error)

	// start the subscriber for spec/status updates
	go o.receiveFromSubscriber(ctx, o.subscriber, fn, errChan)

	// start the resync subscriber for resync events
	go o.receiveFromSubscriber(ctx, o.resyncSubscriber, fn, errChan)

	// Return the error from either subscriber (including context cancellation).
	// We return errors directly instead of writing to the transport errorChan because
	// Pub/Sub client has internal retry logic for transient errors. Only non-retryable
	// errors or context cancellation will be returned here.
	return <-errChan
}

// receiveFromSubscriber handles receiving messages from a subscriber.
func (o *pubsubTransport) receiveFromSubscriber(
	ctx context.Context,
	subscriber *pubsub.Subscriber,
	fn options.ReceiveHandlerFn,
	errChan chan<- error,
) {
	logger := klog.FromContext(ctx)
	err := subscriber.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		evt, err := Decode(msg)
		if err != nil {
			// also send ACK on decode error since redelivery won't fix it.
			logger.Error(err, "failed to decode pubsub message")
		} else {
			fn(ctx, evt)
		}
		// send ACK after all receiver handlers complete.
		msg.Ack()
	})

	// The Pub/Sub client's Receive call automatically retries on retryable errors.
	// See: https://github.com/googleapis/google-cloud-go/blob/b8e70aa0056a3e126bc36cb7bf242d987f32c0bd/pubsub/service.go#L51
	// If Receive returns an error, it's usually due to a non-retryable issue (e.g., subscription not found),
	// service outage, or context cancellation.
	select {
	case errChan <- err:
	default:
	}
}

func (o *pubsubTransport) Close(ctx context.Context) error {
	var err error
	if o.client != nil {
		err = o.client.Close()
	}

	if o.grpcConn != nil {
		_ = o.grpcConn.Close()
		o.grpcConn = nil
	}

	return err
}

func (o *pubsubTransport) ErrorChan() <-chan error {
	return o.errorChan
}

// toGRPCKeepaliveParameter converts our KeepaliveSettings to GRPC ClientParameters.
func toGRPCKeepaliveParameter(settings *KeepaliveSettings) keepalive.ClientParameters {
	return keepalive.ClientParameters{
		PermitWithoutStream: settings.PermitWithoutStream,
		Time:                settings.Time,
		Timeout:             settings.Timeout,
	}
}

// toPubSubReceiveSettings converts our ReceiveSettings to Pub/Sub ReceiveSettings.
func toPubSubReceiveSettings(settings *ReceiveSettings) pubsub.ReceiveSettings {
	receiveSettings := pubsub.ReceiveSettings{}

	if settings.MaxExtension > 0 {
		receiveSettings.MaxExtension = settings.MaxExtension
	}
	if settings.MaxDurationPerAckExtension > 0 {
		receiveSettings.MaxDurationPerAckExtension = settings.MaxDurationPerAckExtension
	}
	if settings.MinDurationPerAckExtension > 0 {
		receiveSettings.MinDurationPerAckExtension = settings.MinDurationPerAckExtension
	}
	if settings.MaxOutstandingMessages > 0 {
		receiveSettings.MaxOutstandingMessages = settings.MaxOutstandingMessages
	}
	if settings.MaxOutstandingBytes > 0 {
		receiveSettings.MaxOutstandingBytes = settings.MaxOutstandingBytes
	}
	if settings.NumGoroutines > 0 {
		receiveSettings.NumGoroutines = settings.NumGoroutines
	}

	return receiveSettings
}
