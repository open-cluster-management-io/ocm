package events

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
)

// ContextualLoggingEventRecorder implements a recorder with contextual logging
type ContextualLoggingEventRecorder struct {
	component string
}

// NewContextualLoggingEventRecorder provides event recorder that will log all recorded events via klog.
func NewContextualLoggingEventRecorder(component string) Recorder {
	return &ContextualLoggingEventRecorder{
		component: component,
	}
}

func (r *ContextualLoggingEventRecorder) ComponentName() string {
	return r.component
}

func (r *ContextualLoggingEventRecorder) ForComponent(component string) Recorder {
	newRecorder := *r
	newRecorder.component = component
	return &newRecorder
}

func (r *ContextualLoggingEventRecorder) Shutdown() {}

func (r *ContextualLoggingEventRecorder) WithComponentSuffix(suffix string) Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *ContextualLoggingEventRecorder) Event(ctx context.Context, reason, message string) {
	logger := klog.FromContext(ctx)
	logger.Info(fmt.Sprintf("INFO: %s", message), "component", r.component, "reason", reason)
}

func (r *ContextualLoggingEventRecorder) Eventf(ctx context.Context, reason, messageFmt string, args ...interface{}) {
	r.Event(ctx, reason, fmt.Sprintf(messageFmt, args...))
}

func (r *ContextualLoggingEventRecorder) Warning(ctx context.Context, reason, message string) {
	logger := klog.FromContext(ctx)
	logger.Info(fmt.Sprintf("WARNING: %s", message), "component", r.component, "reason", reason)
}

func (r *ContextualLoggingEventRecorder) Warningf(ctx context.Context, reason, messageFmt string, args ...interface{}) {
	r.Warning(ctx, reason, fmt.Sprintf(messageFmt, args...))
}
