package event

import (
	"context"

	eventv1 "k8s.io/api/events/v1"
	eventv1client "k8s.io/client-go/kubernetes/typed/events/v1"
	"k8s.io/client-go/rest"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
)

type EventV1ClientWrapper struct {
	EventClient *EventClient
}

func (e EventV1ClientWrapper) RESTClient() rest.Interface {
	panic("RESTClient is unsupported")
}

func (e EventV1ClientWrapper) Events(namespace string) eventv1client.EventInterface {
	return e.EventClient.WithNamespace(namespace)
}

var _ eventv1client.EventsV1Interface = &EventV1ClientWrapper{}

func NewClientHolder(ctx context.Context, opt *options.GenericClientOptions[*eventv1.Event]) (*EventV1ClientWrapper, error) {
	cloudEventsClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	eventClient := NewEventClient(cloudEventsClient)

	return &EventV1ClientWrapper{EventClient: eventClient}, nil
}
