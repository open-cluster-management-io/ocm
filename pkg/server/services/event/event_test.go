package event

import (
	"context"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestList(t *testing.T) {
	service := NewEventService(kubefake.NewSimpleClientset())
	if _, err := service.List(context.Background(), types.ListOptions{}); err == nil {
		t.Errorf("expected error, but failed")
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	cases := []struct {
		name            string
		events          []runtime.Object
		eventEvt        *cloudevents.Event
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedError   bool
	}{
		{
			name:   "invalid event type",
			events: []runtime.Object{},
			eventEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{}).NewEvent()
				return &evt
			}(),
			expectedError: true,
		},
		{
			name:   "invalid action",
			events: []runtime.Object{},
			eventEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: eventce.EventEventDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.DeleteRequestAction,
				}).NewEvent()
				event := &eventsv1.Event{
					ObjectMeta: metav1.ObjectMeta{Name: "test-event", Namespace: "test-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, event)
				return &evt
			}(),
			expectedError: true,
		},
		{
			name:   "create event",
			events: []runtime.Object{},
			eventEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: eventce.EventEventDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.CreateRequestAction,
				}).NewEvent()
				event := &eventsv1.Event{
					ObjectMeta: metav1.ObjectMeta{Name: "test-event", Namespace: "test-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, event)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
		{
			name: "update event",
			events: []runtime.Object{
				&eventsv1.Event{
					ObjectMeta: metav1.ObjectMeta{Name: "test-event", Namespace: "test-namespace"},
				},
			},
			eventEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: eventce.EventEventDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.UpdateRequestAction,
				}).NewEvent()
				event := &eventsv1.Event{
					ObjectMeta: metav1.ObjectMeta{Name: "test-event", Namespace: "test-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, event)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
				if len(actions[1].GetSubresource()) != 0 {
					t.Errorf("unexpected subresource %s", actions[0].GetSubresource())
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.events...)

			service := NewEventService(kubeClient)
			err := service.HandleStatusUpdate(context.Background(), c.eventEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			c.validateActions(t, kubeClient.Actions())
		})
	}
}
