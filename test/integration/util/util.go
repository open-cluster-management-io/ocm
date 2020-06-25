package util

import (
	"fmt"

	"github.com/onsi/ginkgo"

	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"

	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
)

func NewIntegrationTestEventRecorder(componet string) events.Recorder {
	return &IntegrationTestEventRecorder{component: componet}
}

type IntegrationTestEventRecorder struct {
	component string
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

func HasCondition(conditions []operatorapiv1.StatusCondition, expectedType, expectedReason string, expectedStatus metav1.ConditionStatus) bool {
	found := false
	for _, condition := range conditions {
		fmt.Printf(">>> %v \n", condition)
		if condition.Type != expectedType {
			continue
		}
		found = true

		if condition.Status != expectedStatus {
			return false
		}

		if condition.Reason != expectedReason {
			return false
		}

		return true
	}

	return found
}

func NewKubeConfig(host string) []byte {
	configData, _ := runtime.Encode(clientcmdlatest.Codec, &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"test-cluster": {
			Server:                host,
			InsecureSkipTLSVerify: true,
		}},
		Contexts: map[string]*clientcmdapi.Context{"test-context": {
			Cluster: "test-cluster",
		}},
		CurrentContext: "test-context",
	})
	return configData
}
