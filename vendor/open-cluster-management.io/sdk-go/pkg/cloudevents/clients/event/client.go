package event

import (
	"context"

	eventv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	applyconfigurationseventsv1 "k8s.io/client-go/applyconfigurations/events/v1"
	eventv1client "k8s.io/client-go/kubernetes/typed/events/v1"
	"k8s.io/klog/v2"

	cloudeventserrors "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type EventClient struct {
	cloudEventsClient generic.CloudEventsClient[*eventv1.Event]
	namespace         string
}

func NewEventClient(cloudEventsClient generic.CloudEventsClient[*eventv1.Event]) *EventClient {
	return &EventClient{
		cloudEventsClient: cloudEventsClient,
	}
}

func (e *EventClient) WithNamespace(namespace string) *EventClient {
	e.namespace = namespace
	return e
}

func (e *EventClient) Create(ctx context.Context, event *eventv1.Event, opts metav1.CreateOptions) (*eventv1.Event, error) {
	klog.V(4).Infof("creating Event %s", event.Name)
	eventType := types.CloudEventsType{
		CloudEventsDataType: EventEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.CreateRequestAction,
	}

	if err := e.cloudEventsClient.Publish(ctx, eventType, event); err != nil {
		return nil, cloudeventserrors.ToStatusError(eventv1.Resource("events"), event.Name, err)
	}

	return event.DeepCopy(), nil
}

func (e *EventClient) Update(ctx context.Context, event *eventv1.Event, opts metav1.UpdateOptions) (*eventv1.Event, error) {
	return nil, errors.NewMethodNotSupported(eventv1.Resource("events"), "update")
}

func (e *EventClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.NewMethodNotSupported(eventv1.Resource("events"), "delete")
}

func (e *EventClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.NewMethodNotSupported(eventv1.Resource("events"), "deletecollection")
}

func (e *EventClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*eventv1.Event, error) {
	return nil, errors.NewMethodNotSupported(eventv1.Resource("events"), "get")
}

func (e *EventClient) List(ctx context.Context, opts metav1.ListOptions) (*eventv1.EventList, error) {
	return nil, errors.NewMethodNotSupported(eventv1.Resource("events"), "list")
}

func (e *EventClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.NewMethodNotSupported(eventv1.Resource("events"), "watch")
}

func (e *EventClient) Patch(ctx context.Context, name string, pt kubetypes.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *eventv1.Event, err error) {
	// This will be called when recording isomorphic events (https://github.com/kubernetes/client-go/blob/master/tools/events/event_broadcaster.go#L58C6-L58C15)
	// The event broadcaster will only increase the event series.
	// Only publish the event series with an event patch
	patchedEvent, err := utils.Patch(pt, &eventv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: e.namespace,
		}}, data)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	newEvent := patchedEvent.DeepCopy()
	if err := e.cloudEventsClient.Publish(
		ctx,
		types.CloudEventsType{
			CloudEventsDataType: EventEventDataType,
			SubResource:         types.SubResourceSpec,
			Action:              types.UpdateRequestAction,
		},
		newEvent,
	); err != nil {
		return nil, cloudeventserrors.ToStatusError(eventv1.Resource("events"), name, err)
	}

	return newEvent, nil
}

func (e *EventClient) Apply(ctx context.Context, event *applyconfigurationseventsv1.EventApplyConfiguration, opts metav1.ApplyOptions) (result *eventv1.Event, err error) {
	return nil, errors.NewMethodNotSupported(eventv1.Resource("events"), "apply")
}

var _ eventv1client.EventInterface = &EventClient{}
