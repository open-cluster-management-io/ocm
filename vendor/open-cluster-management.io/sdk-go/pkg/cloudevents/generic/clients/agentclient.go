package clients

import (
	"context"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/metrics"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/utils"
)

// CloudEventAgentClient is a client for an agent to resync/send/receive its resources with cloud events.
//
// An agent is a component that handles the deployment of requested resources on the managed cluster and status report
// to the source.
type CloudEventAgentClient[T generic.ResourceObject] struct {
	*baseClient
	lister           generic.Lister[T]
	codec            generic.Codec[T]
	statusHashGetter generic.StatusHashGetter[T]
	agentID          string
	clusterName      string
}

// NewCloudEventAgentClient returns an instance for CloudEventAgentClient. The following arguments are required to
// create a client.
//   - agentOptions provides the clusterName and agentID and the cloudevents clients that are based on different event
//     protocols for sending/receiving the cloudevents.
//   - lister gets the resources from a cache/store of an agent.
//   - statusHashGetter calculates the resource status hash.
//   - codec is used to encode/decode a resource objet/cloudevent to/from a cloudevent/resource objet.
func NewCloudEventAgentClient[T generic.ResourceObject](
	ctx context.Context,
	agentOptions *options.CloudEventsAgentOptions,
	lister generic.Lister[T],
	statusHashGetter generic.StatusHashGetter[T],
	codec generic.Codec[T],
) (generic.CloudEventsClient[T], error) {
	baseClient := newBaseClient(agentOptions.AgentID, agentOptions.CloudEventsTransport, agentOptions.EventRateLimit)
	if err := baseClient.connect(ctx); err != nil {
		return nil, err
	}

	return &CloudEventAgentClient[T]{
		baseClient:       baseClient,
		lister:           lister,
		codec:            codec,
		statusHashGetter: statusHashGetter,
		agentID:          agentOptions.AgentID,
		clusterName:      agentOptions.ClusterName,
	}, nil
}

// SubscribedChan returns a chan which indicates the agent client is subscribed.
// The agent client callers should consider sending a resync request when receiving this signal.
func (c *CloudEventAgentClient[T]) SubscribedChan() <-chan struct{} {
	return c.subscribedChan
}

// Resync the resources spec by sending a spec resync request from the current to the given source.
func (c *CloudEventAgentClient[T]) Resync(ctx context.Context, source string) error {
	// list the resource objects that are maintained by the current agent with the given source
	options := types.ListOptions{Source: source, ClusterName: c.clusterName, CloudEventsDataType: c.codec.EventDataType()}
	objs, err := c.lister.List(ctx, options)
	if err != nil {
		return err
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: c.codec.EventDataType(),
		SubResource:         types.SubResourceSpec,
		Action:              types.ResyncRequestAction,
	}

	resources := &payload.ResourceVersionList{Versions: make([]payload.ResourceVersion, len(objs))}
	for i, obj := range objs {
		rv, err := utils.GetResourceVersionFromObject(eventType, obj)
		if err != nil {
			return err
		}

		resources.Versions[i] = payload.ResourceVersion{
			ResourceID:      string(obj.GetUID()),
			ResourceVersion: rv,
		}
	}

	evt := types.NewEventBuilder(c.agentID, eventType).
		WithOriginalSource(source).
		WithClusterName(c.clusterName).
		NewEvent()
	if err := evt.SetData(cloudevents.ApplicationJSON, resources); err != nil {
		return fmt.Errorf("failed to set data to cloud event: %v", err)
	}

	if err := c.publish(ctx, evt); err != nil {
		return err
	}

	metrics.IncreaseCloudEventsSentFromAgentCounter(evt.Source(), source, c.codec.EventDataType().String(), string(eventType.SubResource), string(eventType.Action))

	return nil
}

// Publish a resource status from an agent to a source.
func (c *CloudEventAgentClient[T]) Publish(ctx context.Context, eventType types.CloudEventsType, obj T) error {
	if eventType.CloudEventsDataType != c.codec.EventDataType() {
		return fmt.Errorf("unsupported cloudevent data type %s", eventType.CloudEventsDataType)
	}

	evt, err := c.codec.Encode(c.agentID, eventType, obj)
	if err != nil {
		return err
	}

	if err := c.publish(ctx, *evt); err != nil {
		return err
	}

	originalSource, _ := cloudeventstypes.ToString(evt.Context.GetExtensions()[types.ExtensionOriginalSource])
	metrics.IncreaseCloudEventsSentFromAgentCounter(evt.Source(), originalSource, eventType.CloudEventsDataType.String(), string(eventType.SubResource), string(eventType.Action))

	return nil
}

// Subscribe the events that are from the source status resync request or source resource spec request.
// For status resync request, agent publish the current resources status back as response.
// For resource spec request, agent receives resource spec and handles the spec with resource handlers.
func (c *CloudEventAgentClient[T]) Subscribe(ctx context.Context, handlers ...generic.ResourceHandler[T]) {
	c.subscribe(ctx, func(ctx context.Context, evt cloudevents.Event) {
		c.receive(ctx, evt, handlers...)
	})
}

func (c *CloudEventAgentClient[T]) receive(ctx context.Context, evt cloudevents.Event, handlers ...generic.ResourceHandler[T]) {
	logger := klog.FromContext(ctx)

	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		logger.Error(err, "failed to parse cloud event type", "eventType", evt.Type())
		return
	}
	logger = logger.WithValues("eventType", evt.Type())

	metrics.IncreaseCloudEventsReceivedByAgentCounter(evt.Source(), eventType.CloudEventsDataType.String(), string(eventType.SubResource), string(eventType.Action))

	if eventType.Action == types.ResyncRequestAction {
		if eventType.SubResource != types.SubResourceStatus {
			logger.Info("ignore unsupported resync event type")
			return
		}

		startTime := time.Now()
		if err := c.respondResyncStatusRequest(ctx, eventType.CloudEventsDataType, evt); err != nil {
			logger.Error(err, "failed to resync manifestsstatus.")
		}
		metrics.UpdateResourceStatusResyncDurationMetric(evt.Source(), c.clusterName, eventType.CloudEventsDataType.String(), startTime)

		return
	}

	if eventType.SubResource != types.SubResourceSpec {
		logger.Info("ignore unsupported event type")
		return
	}

	evtExtensions := evt.Context.GetExtensions()
	clusterName, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionClusterName])
	if err != nil {
		logger.Error(err, "failed to get clustername extension")
		return
	}
	if clusterName != c.clusterName {
		logger.V(4).Info("event clustername and agent clustername do not match, ignore",
			"eventClusterName", clusterName, "agentClusterName", c.clusterName)
		return
	}

	if eventType.CloudEventsDataType != c.codec.EventDataType() {
		logger.Info("unsupported event data type, ignore", "eventDataType", eventType.CloudEventsDataType)
		return
	}

	obj, err := c.codec.Decode(&evt)
	if err != nil {
		logger.Error(err, "failed to decode spec")
		return
	}

	for _, handler := range handlers {
		if err := handler(ctx, obj); err != nil {
			if logger.V(4).Enabled() {
				evtData, _ := evt.MarshalJSON()
				logger.Error(err, "failed to handle spec event", "event", string(evtData))
			} else {
				logger.Error(err, "failed to handle spec event")
			}
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
	ctx context.Context, eventDataType types.CloudEventsDataType, evt cloudevents.Event,
) error {
	logger := klog.FromContext(ctx).WithValues("eventDataType", eventDataType.String())

	options := types.ListOptions{ClusterName: c.clusterName, Source: evt.Source(), CloudEventsDataType: eventDataType}
	objs, err := c.lister.List(ctx, options)
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
			logger.Info("The resource is not found from the source, ignore", "uid", obj.GetUID())
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

func getObj[T generic.ResourceObject](resourceID string, objs []T) (obj T, exists bool) {
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
