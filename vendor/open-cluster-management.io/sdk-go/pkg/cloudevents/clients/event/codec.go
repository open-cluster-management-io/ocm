package event

import (
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var EventEventDataType = types.CloudEventsDataType{
	Group:    eventsv1.GroupName,
	Version:  "v1",
	Resource: "events",
}

// EventCodec is a codec to encode/decode a event/cloudevent for an agent.
type EventCodec struct{}

func NewEventCodec() *EventCodec {
	return &EventCodec{}
}

// EventDataType always returns the event data type `io.k8s.events.v1.events`.
func (c *EventCodec) EventDataType() types.CloudEventsDataType {
	return EventEventDataType
}

// Encode the event to a cloudevent
func (c *EventCodec) Encode(source string, eventType types.CloudEventsType, event *eventsv1.Event) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != EventEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(event.Name).
		WithClusterName(event.Namespace).
		NewEvent()

	if event.ResourceVersion != "" {
		evt.SetExtension(types.ExtensionResourceVersion, event.ResourceVersion)
	}

	newEvent := event.DeepCopy()
	newEvent.TypeMeta = metav1.TypeMeta{
		APIVersion: eventsv1.GroupName + "/v1",
		Kind:       "Event",
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, newEvent); err != nil {
		return nil, fmt.Errorf("failed to encode lease to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent to an event object
func (c *EventCodec) Decode(evt *cloudevents.Event) (*eventsv1.Event, error) {
	event := &eventsv1.Event{}
	if err := evt.DataAs(event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	return event, nil
}
