package util

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewIntegrationTestEventRecorder(component string) events.Recorder {
	return &IntegrationTestEventRecorder{component: component}
}

type IntegrationTestEventRecorder struct {
	component string
	ctx       context.Context
}

func (r *IntegrationTestEventRecorder) ComponentName() string {
	return r.component
}

func (r *IntegrationTestEventRecorder) ForComponent(c string) events.Recorder {
	return &IntegrationTestEventRecorder{component: c}
}

func (r *IntegrationTestEventRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *IntegrationTestEventRecorder) WithContext(ctx context.Context) events.Recorder {
	r.ctx = ctx
	return r
}

func (r *IntegrationTestEventRecorder) Event(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Event: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *IntegrationTestEventRecorder) Warning(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Warning: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *IntegrationTestEventRecorder) Shutdown() {}

func GetCondition(conditions []metav1.Condition, expectedType string) (metav1.Condition, bool) {
	for _, condition := range conditions {
		if condition.Type == expectedType {
			return condition, true
		}
	}
	return metav1.Condition{}, false
}

func MatchCondition(condition metav1.Condition, expected metav1.Condition) bool {
	if len(expected.Type) > 0 && condition.Type != expected.Type {
		return false
	}

	if len(expected.Reason) > 0 && condition.Reason != expected.Reason {
		return false
	}

	if len(expected.Status) > 0 && condition.Status != expected.Status {
		return false
	}

	if expected.ObservedGeneration != 0 && condition.ObservedGeneration != expected.ObservedGeneration {
		return false
	}

	var zero time.Time
	if expected.LastTransitionTime.Time != zero && condition.LastTransitionTime != expected.LastTransitionTime {
		return false
	}

	return true
}

func HasCondition(
	conditions []metav1.Condition,
	expectedType, expectedReason string,
	expectedStatus metav1.ConditionStatus,
) bool {

	condition, ok := GetCondition(conditions, expectedType)
	if !ok {
		return false
	}

	return MatchCondition(condition, metav1.Condition{
		Type:   expectedType,
		Reason: expectedReason,
		Status: expectedStatus,
	})
}
