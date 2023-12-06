package generic

import (
	"context"
	"fmt"
	"strconv"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"k8s.io/klog/v2"

	"open-cluster-management.io/api/cloudevents/generic/options"
	"open-cluster-management.io/api/cloudevents/generic/payload"
	"open-cluster-management.io/api/cloudevents/generic/types"
)

// CloudEventAgentClient is a client for an agent to resync/send/receive its resources with cloud events.
//
// An agent is a component that handles the deployment of requested resources on the managed cluster and status report
// to the source.
type CloudEventAgentClient[T ResourceObject] struct {
	*baseClient
	lister           Lister[T]
	codecs           map[types.CloudEventsDataType]Codec[T]
	statusHashGetter StatusHashGetter[T]
	agentID          string
	clusterName      string
}

// NewCloudEventAgentClient returns an instance for CloudEventAgentClient. The following arguments are required to
// create a client.
//   - agentOptions provides the clusterName and agentID and the cloudevents clients that are based on different event
//     protocols for sending/receiving the cloudevents.
//   - lister gets the resources from a cache/store of an agent.
//   - statusHashGetter calculates the resource status hash.
//   - codecs is list of codecs for encoding/decoding a resource objet/cloudevent to/from a cloudevent/resource objet.
func NewCloudEventAgentClient[T ResourceObject](
	ctx context.Context,
	agentOptions *options.CloudEventsAgentOptions,
	lister Lister[T],
	statusHashGetter StatusHashGetter[T],
	codecs ...Codec[T],
) (*CloudEventAgentClient[T], error) {
	baseClient := &baseClient{
		cloudEventsOptions:     agentOptions.CloudEventsOptions,
		cloudEventsRateLimiter: NewRateLimiter(agentOptions.EventRateLimit),
	}

	if err := baseClient.connect(ctx); err != nil {
		return nil, err
	}

	evtCodes := make(map[types.CloudEventsDataType]Codec[T])
	for _, codec := range codecs {
		evtCodes[codec.EventDataType()] = codec
	}

	return &CloudEventAgentClient[T]{
		baseClient:       baseClient,
		lister:           lister,
		codecs:           evtCodes,
		statusHashGetter: statusHashGetter,
		agentID:          agentOptions.AgentID,
		clusterName:      agentOptions.ClusterName,
	}, nil
}

// Resync the resources spec by sending a spec resync request from an agent to all sources.
func (c *CloudEventAgentClient[T]) Resync(ctx context.Context) error {
	// list the resource objects that are maintained by the current agent from all sources
	objs, err := c.lister.List(types.ListOptions{ClusterName: c.clusterName, Source: types.SourceAll})
	if err != nil {
		return err
	}

	resources := &payload.ResourceVersionList{Versions: make([]payload.ResourceVersion, len(objs))}
	for i, obj := range objs {
		resourceVersion, err := strconv.ParseInt(obj.GetResourceVersion(), 10, 64)
		if err != nil {
			return err
		}

		resources.Versions[i] = payload.ResourceVersion{
			ResourceID:      string(obj.GetUID()),
			ResourceVersion: resourceVersion,
		}
	}

	// only resync the resources whose event data type is registered
	for eventDataType := range c.codecs {
		eventType := types.CloudEventsType{
			CloudEventsDataType: eventDataType,
			SubResource:         types.SubResourceSpec,
			Action:              types.ResyncRequestAction,
		}

		evt := types.NewEventBuilder(c.agentID, eventType).WithClusterName(c.clusterName).NewEvent()
		if err := evt.SetData(cloudevents.ApplicationJSON, resources); err != nil {
			return fmt.Errorf("failed to set data to cloud event: %v", err)
		}

		if err := c.publish(ctx, evt); err != nil {
			return err
		}
	}

	return nil
}

// Publish a resource status from an agent to a source.
func (c *CloudEventAgentClient[T]) Publish(ctx context.Context, eventType types.CloudEventsType, obj T) error {
	codec, ok := c.codecs[eventType.CloudEventsDataType]
	if !ok {
		return fmt.Errorf("failed to find a codec for event %s", eventType.CloudEventsDataType)
	}

	if eventType.SubResource != types.SubResourceStatus {
		return fmt.Errorf("unsupported event eventType %s", eventType)
	}

	evt, err := codec.Encode(c.agentID, eventType, obj)
	if err != nil {
		return err
	}

	if err := c.publish(ctx, *evt); err != nil {
		return err
	}

	return nil
}

// Subscribe the events that are from the source status resync request or source resource spec request.
// For status resync request, agent publish the current resources status back as response.
// For resource spec request, agent receives resource spec and handles the spec with resource handlers.
func (c *CloudEventAgentClient[T]) Subscribe(ctx context.Context, handlers ...ResourceHandler[T]) {
	c.subscribe(ctx, func(ctx context.Context, evt cloudevents.Event) {
		c.receive(ctx, evt, handlers...)
	})
}

func (c *CloudEventAgentClient[T]) receive(ctx context.Context, evt cloudevents.Event, handlers ...ResourceHandler[T]) {
	klog.V(4).Infof("Received event:\n%s", evt)

	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		klog.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
		return
	}

	if eventType.Action == types.ResyncRequestAction {
		if eventType.SubResource != types.SubResourceStatus {
			klog.Warningf("unsupported resync event type %s, ignore", eventType)
			return
		}

		if err := c.respondResyncStatusRequest(ctx, eventType.CloudEventsDataType, evt); err != nil {
			klog.Errorf("failed to resync manifestsstatus, %v", err)
		}

		return
	}

	if eventType.SubResource != types.SubResourceSpec {
		klog.Warningf("unsupported event type %s, ignore", eventType)
		return
	}

	codec, ok := c.codecs[eventType.CloudEventsDataType]
	if !ok {
		klog.Warningf("failed to find the codec for event %s, ignore", eventType.CloudEventsDataType)
		return
	}

	obj, err := codec.Decode(&evt)
	if err != nil {
		klog.Errorf("failed to decode spec, %v", err)
		return
	}

	action, err := c.specAction(evt.Source(), obj)
	if err != nil {
		klog.Errorf("failed to generate spec action %s, %v", evt, err)
		return
	}

	if len(action) == 0 {
		// no action is required, ignore
		return
	}

	for _, handler := range handlers {
		if err := handler(action, obj); err != nil {
			klog.Errorf("failed to handle spec event %s, %v", evt, err)
		}
	}
}

// Upon receiving the status resync event, the agent responds by sending resource status events to the broker as
// follows:
//   - If the event payload is empty, the agent returns the status of all resources it maintains.
//   - If the event payload is not empty, the agent retrieves the resource with the specified ID and compares the
//     received resource status hash with the current resource status hash. If they are not equal, the agent sends the
//     resource status message.
func (c *CloudEventAgentClient[T]) respondResyncStatusRequest(
	ctx context.Context, eventDataType types.CloudEventsDataType, evt cloudevents.Event) error {
	objs, err := c.lister.List(types.ListOptions{ClusterName: c.clusterName, Source: evt.Source()})
	if err != nil {
		return err
	}

	statusHashes, err := payload.DecodeStatusResyncRequest(evt)
	if err != nil {
		return err
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: eventDataType,
		SubResource:         types.SubResourceStatus,
		Action:              types.ResyncResponseAction,
	}

	if len(statusHashes.Hashes) == 0 {
		// publish all resources status
		for _, obj := range objs {
			if err := c.Publish(ctx, eventType, obj); err != nil {
				return err
			}
		}

		return nil
	}

	for _, obj := range objs {
		lastHash, ok := findStatusHash(string(obj.GetUID()), statusHashes.Hashes)
		if !ok {
			// ignore the resource that is not on the source, but exists on the agent, wait for the source deleting it
			klog.Infof("The resource %s is not found from the source, ignore", obj.GetUID())
			continue
		}

		currentHash, err := c.statusHashGetter(obj)
		if err != nil {
			continue
		}

		if currentHash == lastHash {
			// the status is not changed, do nothing
			continue
		}

		if err := c.Publish(ctx, eventType, obj); err != nil {
			return err
		}
	}

	return nil
}

func (c *CloudEventAgentClient[T]) specAction(source string, obj T) (evt types.ResourceAction, err error) {
	objs, err := c.lister.List(types.ListOptions{ClusterName: c.clusterName, Source: source})
	if err != nil {
		return evt, err
	}

	lastObj, exists := getObj(string(obj.GetUID()), objs)
	if !exists {
		return types.Added, nil
	}

	if !obj.GetDeletionTimestamp().IsZero() {
		return types.Deleted, nil
	}

	if obj.GetResourceVersion() == lastObj.GetResourceVersion() {
		return evt, nil
	}

	return types.Modified, nil
}

func getObj[T ResourceObject](resourceID string, objs []T) (obj T, exists bool) {
	for _, obj := range objs {
		if string(obj.GetUID()) == resourceID {
			return obj, true
		}
	}

	return obj, false
}

func findStatusHash(id string, hashes []payload.ResourceStatusHash) (string, bool) {
	for _, hash := range hashes {
		if id == hash.ResourceID {
			return hash.StatusHash, true
		}
	}

	return "", false
}
