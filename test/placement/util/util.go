package util

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewIntegrationTestEventRecorder(componet string) events.Recorder {
	return &IntegrationTestEventRecorder{component: componet}
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

func (r *IntegrationTestEventRecorder) Shutdown() {
	return
}

func HasCondition(
	conditions []metav1.Condition,
	expectedType, expectedReason string,
	expectedStatus metav1.ConditionStatus,
) bool {
	found := false
	for _, condition := range conditions {
		if condition.Type != expectedType {
			continue
		}
		found = true

		if condition.Status != expectedStatus {
			return false
		}

		// skip checking reason
		if len(expectedReason) == 0 {
			return true
		}

		if condition.Reason != expectedReason {
			return false
		}

		return true
	}

	return found
}
