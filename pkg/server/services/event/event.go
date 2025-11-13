package event

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	eventv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

type EventService struct {
	client kubernetes.Interface
	codec  *eventce.EventCodec
}

func NewEventService(client kubernetes.Interface) server.Service {
	return &EventService{
		client: client,
		codec:  eventce.NewEventCodec(),
	}
}

func (e *EventService) Get(ctx context.Context, resourceID string) (*cloudevents.Event, error) {
	return nil, errors.NewMethodNotSupported(eventv1.Resource("events"), "get")
}

func (e *EventService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	return nil, errors.NewMethodNotSupported(eventv1.Resource("events"), "list")
}

func (e *EventService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	logger := klog.FromContext(ctx)

	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	event, err := e.codec.Decode(evt)
	if err != nil {
		return err
	}

	logger.V(4).Info("handle event",
		"eventNamespace", event.Namespace, "eventName", event.Name,
		"subResource", eventType.SubResource, "actionType", eventType.Action)

	switch eventType.Action {
	case types.CreateRequestAction:
		_, err := e.client.EventsV1().Events(event.Namespace).Create(ctx, event, metav1.CreateOptions{})
		return err
	case types.UpdateRequestAction:
		last, err := e.client.EventsV1().Events(event.Namespace).Get(ctx, event.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// event is not found, do nothing
			return nil
		}
		if err != nil {
			return err
		}

		// only update the series field
		updated := last.DeepCopy()
		updated.Series = event.Series

		_, err = e.client.EventsV1().Events(event.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
		return err
	default:
		return fmt.Errorf("unsupported action %s for event %s/%s", eventType.Action, event.Namespace, event.Name)
	}
}

func (e *EventService) RegisterHandler(_ context.Context, _ server.EventHandler) {
	// do nothing
}
