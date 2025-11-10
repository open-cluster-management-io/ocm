package recorder

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/klog/v2"
)

// ContextualLoggingEventRecorder implements a recorder with contextual logging
type ContextualLoggingEventRecorder struct {
	component string
	ctx       context.Context
}

func (r *ContextualLoggingEventRecorder) WithContext(ctx context.Context) events.Recorder {
	newRecorder := *r
	newRecorder.ctx = ctx
	return &newRecorder
}

// NewContextualLoggingEventRecorder provides event recorder that will log all recorded events via klog.
func NewContextualLoggingEventRecorder(component string) events.Recorder {
	return &ContextualLoggingEventRecorder{
		component: component,
		ctx:       context.Background(),
	}
}

func (r *ContextualLoggingEventRecorder) ComponentName() string {
	return r.component
}

func (r *ContextualLoggingEventRecorder) ForComponent(component string) events.Recorder {
	newRecorder := *r
	newRecorder.component = component
	return &newRecorder
}

func (r *ContextualLoggingEventRecorder) Shutdown() {}

func (r *ContextualLoggingEventRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *ContextualLoggingEventRecorder) Event(reason, message string) {
	logger := klog.FromContext(r.ctx)
	logger.Info(fmt.Sprintf("INFO: %s", message), "component", r.component, "reason", reason)
}

func (r *ContextualLoggingEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *ContextualLoggingEventRecorder) Warning(reason, message string) {
	logger := klog.FromContext(r.ctx)
	logger.Info(fmt.Sprintf("WARNING: %s", message), "component", r.component, "reason", reason)
}

func (r *ContextualLoggingEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}
