package services

import (
	"context"
	"fmt"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

type EventService struct {
	client kubernetes.Interface
	codec  eventce.EventCodec
}

func (e EventService) Get(ctx context.Context, resourceID string) (*cloudevents.Event, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(resourceID)
	if err != nil {
		return nil, err
	}
	evt, err := e.client.EventsV1().Events(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return e.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: eventce.EventEventDataType}, evt)
}

func (e EventService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	if len(listOpts.ClusterName) == 0 {
		return nil, fmt.Errorf("cluster name is empty")
	}
	evts, err := e.client.EventsV1().Events(listOpts.ClusterName).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var cloudevts []*cloudevents.Event
	for _, evt := range evts.Items {
		cloudevt, err := e.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: eventce.EventEventDataType}, &evt)
		if err != nil {
			return nil, err
		}
		cloudevts = append(cloudevts, cloudevt)
	}
	return cloudevts, nil
}

func (e EventService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	event, err := e.codec.Decode(evt)
	if err != nil {
		return err
	}

	// only create and update action
	switch eventType.Action {
	case createRequestAction:
		_, err := e.client.EventsV1().Events(event.Namespace).Create(ctx, event, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (e EventService) RegisterHandler(handler server.EventHandler) {
	return
}

var _ server.Service = &EventService{}

func NewEventService(client kubernetes.Interface) *EventService {
	return &EventService{
		client: client,
	}
}
