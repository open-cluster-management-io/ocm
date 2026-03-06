package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type ContextTracingKey string

const (
	LogTracingPrefix                        = "logging.open-cluster-management.io/"
	ExtensionLogTracing                     = "logtracing"
	ContextTracingOPIDKey ContextTracingKey = "op-id"
)

// DefaultContextTracingKeys is a global variable of interested keys in context used for log tracing
var DefaultContextTracingKeys = []ContextTracingKey{ContextTracingOPIDKey}

func SetLogTracingByObject(logger klog.Logger, object metav1.Object) klog.Logger {
	if object == nil {
		return logger
	}
	annotations := object.GetAnnotations()
	for key, value := range annotations {
		if !strings.HasPrefix(key, LogTracingPrefix) {
			continue
		}
		tracingKey := strings.TrimPrefix(key, LogTracingPrefix)
		logger = logger.WithValues(tracingKey, value)
	}
	return logger
}

func getTracingMapFromExtension(evt *cloudevents.Event) (map[string]string, error) {
	logTracingMap := make(map[string]string)

	logTracingValue, ok := evt.Extensions()[ExtensionLogTracing]
	if !ok {
		return logTracingMap, nil
	}

	logTracingValueString, err := cloudeventstypes.ToString(logTracingValue)
	if err != nil {
		return logTracingMap, err
	}

	err = json.Unmarshal([]byte(logTracingValueString), &logTracingMap)
	if err != nil {
		return logTracingMap, err
	}

	return logTracingMap, nil
}

func setTracingMapToExtension(evt *cloudevents.Event, tracingMap map[string]string) error {
	tracingValueRaw, err := json.Marshal(tracingMap)
	if err != nil {
		return err
	}
	evt.SetExtension(ExtensionLogTracing, string(tracingValueRaw))
	return nil
}

func SetLogTracingByCloudEvent(logger klog.Logger, evt *cloudevents.Event) klog.Logger {
	if evt == nil {
		return logger
	}

	logTracingMap, err := getTracingMapFromExtension(evt)
	if err != nil {
		logger.Error(err, "Failed to get log tracing map")
		return logger
	}

	for key, value := range logTracingMap {
		if !strings.HasPrefix(key, LogTracingPrefix) {
			continue
		}
		tracingKey := strings.TrimPrefix(key, LogTracingPrefix)
		logger = logger.WithValues(tracingKey, value)
	}
	return logger
}

// SetLogTracingFromContext is used in cloudevent work source client to set up the
// log tracing annotation for the manifestwork.
func SetLogTracingFromContext(ctx context.Context, object metav1.Object) {
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	for _, key := range DefaultContextTracingKeys {
		value := ctx.Value(key)
		if value == nil {
			continue
		}
		annotations[LogTracingPrefix+string(key)] = fmt.Sprintf("%v", value)
	}

	if len(annotations) != 0 {
		object.SetAnnotations(annotations)
	}
}

func LogTracingFromEventToObject(evt *cloudevents.Event, obj metav1.Object) error {
	if obj == nil {
		return nil
	}
	if evt == nil {
		return nil
	}

	logTracingMap, err := getTracingMapFromExtension(evt)
	if err != nil {
		return err
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	for key, value := range logTracingMap {
		if !strings.HasPrefix(key, LogTracingPrefix) {
			continue
		}
		annotations[key] = value
	}

	if len(annotations) != 0 {
		obj.SetAnnotations(annotations)
	}

	return nil
}

func LogTracingFromObjectToEvent(obj metav1.Object, evt *cloudevents.Event) error {
	if obj == nil {
		return nil
	}
	if evt == nil {
		return nil
	}

	tracingMap := make(map[string]string)
	for key, value := range obj.GetAnnotations() {
		if !strings.HasPrefix(key, LogTracingPrefix) {
			continue
		}
		tracingMap[key] = value
	}

	return setTracingMapToExtension(evt, tracingMap)
}
