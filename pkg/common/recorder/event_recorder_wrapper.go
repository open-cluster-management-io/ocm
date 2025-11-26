package recorder

import (
	"context"
	librarygoevents "github.com/openshift/library-go/pkg/operator/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
)

// eventsRecorderWrapper wraps events recorder to a library-go recorder
type EventsRecorderWrapper struct {
	recorder events.Recorder
	ctx      context.Context
}

func NewEventsRecorderWrapper(ctx context.Context, recorder events.Recorder) librarygoevents.Recorder {
	return &EventsRecorderWrapper{
		recorder: recorder,
		ctx:      ctx,
	}
}

func (e *EventsRecorderWrapper) Event(reason, message string) {
	e.recorder.Event(e.ctx, reason, message)
}

func (e *EventsRecorderWrapper) Shutdown() {}

func (e *EventsRecorderWrapper) Eventf(reason, messageFmt string, args ...interface{}) {
	e.recorder.Eventf(e.ctx, reason, messageFmt, args...)
}

func (e *EventsRecorderWrapper) Warning(reason, message string) {
	e.recorder.Warning(e.ctx, reason, message)
}

func (e *EventsRecorderWrapper) Warningf(reason, messageFmt string, args ...interface{}) {
	e.recorder.Warningf(e.ctx, reason, messageFmt, args...)
}

func (e *EventsRecorderWrapper) ForComponent(componentName string) librarygoevents.Recorder {
	e.recorder.ForComponent(componentName)
	return e
}

func (e *EventsRecorderWrapper) WithComponentSuffix(suffix string) librarygoevents.Recorder {
	e.recorder.WithComponentSuffix(suffix)
	return e
}

func (e *EventsRecorderWrapper) WithContext(ctx context.Context) librarygoevents.Recorder {
	eCopy := *e
	eCopy.ctx = ctx
	return e
}

func (e *EventsRecorderWrapper) ComponentName() string {
	return e.recorder.ComponentName()
}
