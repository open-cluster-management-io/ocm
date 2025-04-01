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

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestGet(t *testing.T) {
	cases := []struct {
		name          string
		events        []runtime.Object
		resourceID    string
		expectedError bool
	}{
		{
			name:          "event not found",
			events:        []runtime.Object{},
			resourceID:    "test-namespace/test-event",
			expectedError: true,
		},
		{
			name:       "get event",
			resourceID: "test-namespace/test-event",
			events: []runtime.Object{&eventsv1.Event{
				ObjectMeta: metav1.ObjectMeta{Name: "test-event", Namespace: "test-namespace"},
			}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.events...)

			service := NewEventService(kubeClient)
			_, err := service.Get(context.Background(), c.resourceID)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestList(t *testing.T) {
	cases := []struct {
		name           string
		events         []runtime.Object
		clusterName    string
		expectedEvents int
	}{
		{
			name:           "no events",
			events:         []runtime.Object{},
			clusterName:    "test-cluster",
			expectedEvents: 0,
		},
		{
			name: "list events",
			events: []runtime.Object{
				&eventsv1.Event{
					ObjectMeta: metav1.ObjectMeta{Name: "test-event1", Namespace: "test-cluster1"},
				},
				&eventsv1.Event{
					ObjectMeta: metav1.ObjectMeta{Name: "test-event2", Namespace: "test-cluster2"},
				},
			},
			clusterName:    "test-cluster1",
			expectedEvents: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.events...)

			service := NewEventService(kubeClient)
			evts, err := service.List(types.ListOptions{ClusterName: c.clusterName})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(evts) != c.expectedEvents {
				t.Errorf("expected %d events, got %d", c.expectedEvents, len(evts))
			}
		})
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
				addon := &addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, addon)
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
				testingcommon.AssertActions(t, actions, "update")
				if len(actions[0].GetSubresource()) != 0 {
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
