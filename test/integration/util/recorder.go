package util

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

var (
	// ConditionNotFound can be used to assert a condition type does not exist with ExpectedConditions
	ConditionNotFound metav1.ConditionStatus = "-"
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

	if len(expected.Message) > 0 && condition.Message != expected.Message {
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

func CheckExpectedConditions(conditions []metav1.Condition, expectedConditions ...metav1.Condition) utilerrors.Aggregate {
	var errs []error
	for _, expected := range expectedConditions {
		condition := meta.FindStatusCondition(conditions, expected.Type)
		switch expected.Status {
		case ConditionNotFound:
			if condition != nil {
				errs = append(errs, fmt.Errorf("Expected not to find condition %s, got %+v", expected.Type, condition))
			}
		default:
			if condition == nil || !MatchCondition(*condition, expected) {
				errs = append(errs, fmt.Errorf("Expected to find condition %+v, got %+v", expected, condition))
			}
		}
	}
	if errs == nil {
		return nil
	}
	return utilerrors.NewAggregate(errs)
}

func HasCondition(
	conditions []metav1.Condition,
	expectedType, expectedReason string,
	expectedStatus metav1.ConditionStatus,
) bool {
	condition := meta.FindStatusCondition(conditions, expectedType)
	if condition == nil {
		return false
	}

	return MatchCondition(*condition, metav1.Condition{
		Type:   expectedType,
		Reason: expectedReason,
		Status: expectedStatus,
	})
}
