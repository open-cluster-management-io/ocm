package helpers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	kevents "k8s.io/client-go/tools/events"
)

// NewEventRecorder creates a new event recorder for the given controller, it will also log the events
func NewEventRecorder(ctx context.Context, scheme *runtime.Scheme,
	kubeClient kubernetes.Interface, controllerName string) (kevents.EventRecorder, error) {
	broadcaster := kevents.NewBroadcaster(&kevents.EventSinkImpl{Interface: kubeClient.EventsV1()})
	err := broadcaster.StartRecordingToSinkWithContext(ctx)
	if err != nil {
		return nil, nil
	}
	broadcaster.StartStructuredLogging(0)
	recorder := broadcaster.NewRecorder(scheme, controllerName)
	return recorder, nil
}
