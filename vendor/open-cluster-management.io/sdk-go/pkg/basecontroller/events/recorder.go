package events

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
)

// Recorder is a simple event recording interface.
type Recorder interface {
	Event(ctx context.Context, reason, message string)
	Eventf(ctx context.Context, reason, messageFmt string, args ...interface{})
	Warning(ctx context.Context, reason, message string)
	Warningf(ctx context.Context, reason, messageFmt string, args ...interface{})

	// ForComponent allows to fiddle the component name before sending the event to sink.
	// Making more unique components will prevent the spam filter in upstream event sink from dropping
	// events.
	ForComponent(componentName string) Recorder

	// WithComponentSuffix is similar to ForComponent except it just suffix the current component name instead of overriding.
	WithComponentSuffix(componentNameSuffix string) Recorder

	// ComponentName returns the current source component name for the event.
	// This allows to suffix the original component name with 'sub-component'.
	ComponentName() string

	Shutdown()
}

// NewRecorder returns new event recorder.
func NewRecorder(client corev1client.EventInterface, sourceComponentName string, involvedObjectRef *corev1.ObjectReference) Recorder {
	return &recorder{
		eventClient:       client,
		involvedObjectRef: involvedObjectRef,
		sourceComponent:   sourceComponentName,
	}
}

// recorder is an implementation of Recorder interface.
type recorder struct {
	eventClient       corev1client.EventInterface
	involvedObjectRef *corev1.ObjectReference
	sourceComponent   string
}

func (r *recorder) ComponentName() string {
	return r.sourceComponent
}

func (r *recorder) Shutdown() {}

func (r *recorder) ForComponent(componentName string) Recorder {
	newRecorderForComponent := *r
	newRecorderForComponent.sourceComponent = componentName
	return &newRecorderForComponent
}

func (r *recorder) WithComponentSuffix(suffix string) Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

// Eventf emits the normal type event and allow formatting of message.
func (r *recorder) Eventf(ctx context.Context, reason, messageFmt string, args ...interface{}) {
	r.Event(ctx, reason, fmt.Sprintf(messageFmt, args...))
}

// Warningf emits the warning type event and allow formatting of message.
func (r *recorder) Warningf(ctx context.Context, reason, messageFmt string, args ...interface{}) {
	r.Warning(ctx, reason, fmt.Sprintf(messageFmt, args...))
}

// Event emits the normal type event.
func (r *recorder) Event(ctx context.Context, reason, message string) {
	event := makeEvent(r.involvedObjectRef, r.sourceComponent, corev1.EventTypeNormal, reason, message)
	if _, err := r.eventClient.Create(ctx, event, metav1.CreateOptions{}); err != nil {
		klog.Warningf("Error creating event %+v: %v", event, err)
	}
}

// Warning emits the warning type event.
func (r *recorder) Warning(ctx context.Context, reason, message string) {
	event := makeEvent(r.involvedObjectRef, r.sourceComponent, corev1.EventTypeWarning, reason, message)
	if _, err := r.eventClient.Create(ctx, event, metav1.CreateOptions{}); err != nil {
		klog.Warningf("Error creating event %+v: %v", event, err)
	}
}

func makeEvent(involvedObjRef *corev1.ObjectReference, sourceComponent string, eventType, reason, message string) *corev1.Event {
	currentTime := metav1.Time{Time: time.Now()}
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%v.%x", involvedObjRef.Name, currentTime.UnixNano()),
			Namespace: involvedObjRef.Namespace,
		},
		InvolvedObject: *involvedObjRef,
		Reason:         reason,
		Message:        message,
		Type:           eventType,
		Count:          1,
		FirstTimestamp: currentTime,
		LastTimestamp:  currentTime,
	}
	event.Source.Component = sourceComponent
	return event
}
