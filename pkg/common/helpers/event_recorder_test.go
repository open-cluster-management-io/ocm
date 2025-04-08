package helpers

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	workscheme "open-cluster-management.io/api/client/work/clientset/versioned/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestNewEventRecorder(t *testing.T) {
	tests := []struct {
		name            string
		scheme          *runtime.Scheme
		wait            time.Duration
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:   "test new event recorder, scheme not match ",
			scheme: workscheme.Scheme,
			wait:   100 * time.Millisecond,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:   "test new event recorder, scheme match",
			scheme: clusterscheme.Scheme,
			wait:   100 * time.Millisecond,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
		{
			name:   "test new event recorder, scheme match, no wait",
			scheme: clusterscheme.Scheme,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			kubeClient := fakekube.NewClientset()
			recorder, err := NewEventRecorder(ctx, tt.scheme, kubeClient.EventsV1(), "test")
			if err != nil {
				t.Errorf("NewEventRecorder() error = %v", err)
				return
			}

			object := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			}
			recorder.Eventf(object, nil, corev1.EventTypeNormal, "test", "test", "")
			time.Sleep(tt.wait)
			tt.validateActions(t, kubeClient.Actions())
		})
	}
}
