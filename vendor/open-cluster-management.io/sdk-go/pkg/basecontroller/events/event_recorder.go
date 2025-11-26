package events

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	eventsv1 "k8s.io/client-go/kubernetes/typed/events/v1"
	kevents "k8s.io/client-go/tools/events"
)

// NewEventRecorder creates a new event recorder for the given controller, it will also log the events
func NewEventRecorder(ctx context.Context, scheme *runtime.Scheme,
	eventsClient eventsv1.EventsV1Interface, controllerName string) (kevents.EventRecorder, error) {
	broadcaster := kevents.NewBroadcaster(&kevents.EventSinkImpl{Interface: eventsClient})
	err := broadcaster.StartRecordingToSinkWithContext(ctx)
	if err != nil {
		return nil, err
	}
	broadcaster.StartStructuredLogging(0)
	recorder := broadcaster.NewRecorder(scheme, controllerName)
	return recorder, nil
}
