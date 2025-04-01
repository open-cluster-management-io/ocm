package event

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"

	"open-cluster-management.io/ocm/pkg/common/helpers"
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
	namespace, name, err := cache.SplitMetaNamespaceKey(resourceID)
	if err != nil {
		return nil, err
	}
	evt, err := e.client.EventsV1().Events(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return e.codec.Encode(helpers.CloudEventsKubeSource, types.CloudEventsType{CloudEventsDataType: eventce.EventEventDataType}, evt)
}

func (e *EventService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	evts, err := e.client.EventsV1().Events(listOpts.ClusterName).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var cloudevts []*cloudevents.Event
	for _, evt := range evts.Items {
		cloudevt, err := e.codec.Encode(helpers.CloudEventsKubeSource, types.CloudEventsType{CloudEventsDataType: eventce.EventEventDataType}, &evt)
		if err != nil {
			return nil, err
		}
		cloudevts = append(cloudevts, cloudevt)
	}
	return cloudevts, nil
}

func (e *EventService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	event, err := e.codec.Decode(evt)
	if err != nil {
		return err
	}

	klog.V(4).Infof("event %s/%s %s %s", event.Namespace, event.Name, eventType.SubResource, eventType.Action)

	switch eventType.Action {
	case types.CreateRequestAction:
		_, err := e.client.EventsV1().Events(event.Namespace).Create(ctx, event, metav1.CreateOptions{})
		return err
	case types.UpdateRequestAction:
		_, err := e.client.EventsV1().Events(event.Namespace).Update(ctx, event, metav1.UpdateOptions{})
		return err
	default:
		return fmt.Errorf("unsupported action %s for event %s/%s", eventType.Action, event.Namespace, event.Name)
	}
}

func (e *EventService) RegisterHandler(handler server.EventHandler) {
	// do nothing
}
