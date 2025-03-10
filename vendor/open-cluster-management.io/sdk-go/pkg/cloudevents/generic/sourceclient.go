package generic

import (
	"context"
	"fmt"
	"strconv"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// CloudEventSourceClient is a client for a source to resync/send/receive its resources with cloud events.
//
// A source is a component that runs on a server, it can be a controller on the hub cluster or a RESTful service
// handling resource requests.
type CloudEventSourceClient[T ResourceObject] struct {
	*baseClient
	lister           Lister[T]
	codec            Codec[T]
	statusHashGetter StatusHashGetter[T]
	sourceID         string
}

// NewCloudEventSourceClient returns an instance for CloudEventSourceClient. The following arguments are required to
// create a client
//   - sourceOptions provides the sourceID and the cloudevents clients that are based on different event protocols for
//     sending/receiving the cloudevents.
//   - lister gets the resources from a cache/store of a source.
//   - statusHashGetter calculates the resource status hash.
//   - codec is used to encode/decode a resource objet/cloudevent to/from a cloudevent/resource objet.
func NewCloudEventSourceClient[T ResourceObject](
	ctx context.Context,
	sourceOptions *options.CloudEventsSourceOptions,
	lister Lister[T],
	statusHashGetter StatusHashGetter[T],
	codec Codec[T],
) (*CloudEventSourceClient[T], error) {
	baseClient := &baseClient{
		clientID:               sourceOptions.SourceID,
		cloudEventsOptions:     sourceOptions.CloudEventsOptions,
		cloudEventsRateLimiter: NewRateLimiter(sourceOptions.EventRateLimit),
		reconnectedChan:        make(chan struct{}),
		dataType:               codec.EventDataType(),
	}

	if err := baseClient.connect(ctx); err != nil {
		return nil, err
	}

	return &CloudEventSourceClient[T]{
		baseClient:       baseClient,
		lister:           lister,
		codec:            codec,
		statusHashGetter: statusHashGetter,
		sourceID:         sourceOptions.SourceID,
	}, nil
}

func (c *CloudEventSourceClient[T]) ReconnectedChan() <-chan struct{} {
	return c.reconnectedChan
}

// Resync the resources status by sending a status resync request from the current source to a specified cluster.
func (c *CloudEventSourceClient[T]) Resync(ctx context.Context, clusterName string) error {
	// list the resource objects that are maintained by the current source with a specified cluster
	options := types.ListOptions{Source: c.sourceID, ClusterName: clusterName, CloudEventsDataType: c.codec.EventDataType()}
	objs, err := c.lister.List(options)
	if err != nil {
		return err
	}

	hashes := &payload.ResourceStatusHashList{Hashes: make([]payload.ResourceStatusHash, len(objs))}
	for i, obj := range objs {
		statusHash, err := c.statusHashGetter(obj)
		if err != nil {
			return err
		}

		hashes.Hashes[i] = payload.ResourceStatusHash{
			ResourceID: string(obj.GetUID()),
			StatusHash: statusHash,
		}
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: c.codec.EventDataType(),
		SubResource:         types.SubResourceStatus,
		Action:              types.ResyncRequestAction,
	}

	evt := types.NewEventBuilder(c.sourceID, eventType).WithClusterName(clusterName).NewEvent()
	if err := evt.SetData(cloudevents.ApplicationJSON, hashes); err != nil {
		return fmt.Errorf("failed to set data to cloud event: %v", err)
	}

	if err := c.publish(ctx, evt); err != nil {
		return err
	}

	increaseCloudEventsSentCounter(evt.Source(), "", clusterName, c.codec.EventDataType().String(), string(eventType.SubResource), string(eventType.Action))

	return nil
}

// Publish a resource spec from a source to an agent.
func (c *CloudEventSourceClient[T]) Publish(ctx context.Context, eventType types.CloudEventsType, obj T) error {
	if eventType.CloudEventsDataType != c.codec.EventDataType() {
		return fmt.Errorf("unsupported event data type %s", eventType.CloudEventsDataType)
	}

	if eventType.SubResource != types.SubResourceSpec {
		return fmt.Errorf("unsupported event eventType %s", eventType)
	}

	evt, err := c.codec.Encode(c.sourceID, eventType, obj)
	if err != nil {
		return err
	}

	if err := c.publish(ctx, *evt); err != nil {
		return err
	}

	clusterName := evt.Context.GetExtensions()[types.ExtensionClusterName].(string)
	increaseCloudEventsSentCounter(evt.Source(), "", clusterName, eventType.CloudEventsDataType.String(), string(eventType.SubResource), string(eventType.Action))

	return nil
}

// Subscribe the events that are from the agent spec resync request or agent resource status request.
// For spec resync request, source publish the current resources spec back as response.
// For resource status request, source receives resource status and handles the status with resource handlers.
func (c *CloudEventSourceClient[T]) Subscribe(ctx context.Context, handlers ...ResourceHandler[T]) {
	c.subscribe(ctx, func(ctx context.Context, evt cloudevents.Event) {
		c.receive(ctx, evt, handlers...)
	})
}

func (c *CloudEventSourceClient[T]) receive(ctx context.Context, evt cloudevents.Event, handlers ...ResourceHandler[T]) {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		klog.Errorf("failed to parse cloud event type, %v", err)
		return
	}

	// clusterName is not required for agent to send the request, in case of missing clusterName, set it to
	// empty string, as the source is sufficient to infer the event's originating cluster.
	cn, err := cloudeventstypes.ToString(evt.Context.GetExtensions()[types.ExtensionClusterName])
	if err != nil {
		cn = ""
	}

	increaseCloudEventsReceivedCounter(evt.Source(), cn, eventType.CloudEventsDataType.String(), string(eventType.SubResource), string(eventType.Action))

	if eventType.Action == types.ResyncRequestAction {
		if eventType.SubResource != types.SubResourceSpec {
			klog.Warningf("unsupported event type %s, ignore", eventType)
			return
		}

		clusterName, err := evt.Context.GetExtension(types.ExtensionClusterName)
		if err != nil {
			klog.Errorf("failed to get cluster name extension, %v", err)
			return
		}

		startTime := time.Now()
		if err := c.respondResyncSpecRequest(ctx, eventType.CloudEventsDataType, evt); err != nil {
			klog.Errorf("failed to resync resources spec, %v", err)
		}
		updateResourceSpecResyncDurationMetric(c.sourceID, fmt.Sprintf("%s", clusterName), eventType.CloudEventsDataType.String(), startTime)

		return
	}

	if eventType.CloudEventsDataType != c.codec.EventDataType() {
		klog.Warningf("unsupported event data type %s, ignore", eventType.CloudEventsDataType)
		return
	}

	if eventType.SubResource != types.SubResourceStatus {
		klog.Warningf("unsupported event type %s, ignore", eventType)
		return
	}

	obj, err := c.codec.Decode(&evt)
	if err != nil {
		klog.Errorf("failed to decode status, %v", err)
		return
	}

	for _, handler := range handlers {
		if err := handler(types.StatusModified, obj); err != nil {
			klog.Errorf("failed to handle status event %s, %v", evt, err)
		}
	}
}

// Upon receiving the spec resync event, the source responds by sending resource status events to the broker as follows:
//   - If the request event message is empty, the source returns all resources associated with the work agent.
//   - If the request event message contains resource IDs and versions, the source retrieves the resource with the
//     specified ID and compares the versions.
//   - If the requested resource version matches the source's current maintained resource version, the source does not
//     resend the resource.
//   - If the requested resource version is older than the source's current maintained resource version, the source
//     sends the resource.
func (c *CloudEventSourceClient[T]) respondResyncSpecRequest(
	ctx context.Context, evtDataType types.CloudEventsDataType, evt cloudevents.Event,
) error {
	resourceVersions, err := payload.DecodeSpecResyncRequest(evt)
	if err != nil {
		return err
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: evtDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.ResyncResponseAction,
	}

	clusterName, err := evt.Context.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return err
	}

	options := types.ListOptions{
		ClusterName:         fmt.Sprintf("%s", clusterName),
		Source:              c.sourceID,
		CloudEventsDataType: evtDataType,
	}
	objs, err := c.lister.List(options)
	if err != nil {
		return err
	}

	// TODO we cannot list objs now, the lister may be not ready, we may need to add HasSynced
	// for the lister
	if len(objs) == 0 {
		klog.V(4).Infof("there are is no objs from the list, do nothing")
		return nil
	}

	for _, obj := range objs {
		// respond with the deleting resource regardless of the resource version
		if !obj.GetDeletionTimestamp().IsZero() {
			if err := c.Publish(ctx, eventType, obj); err != nil {
				return err
			}
			continue
		}

		lastResourceVersion := findResourceVersion(string(obj.GetUID()), resourceVersions.Versions)
		currentResourceVersion, err := strconv.ParseInt(obj.GetResourceVersion(), 10, 64)
		if err != nil {
			klog.V(4).Infof("ignore the obj %v since it has a invalid resourceVersion, %v", obj, err)
			continue
		}

		// the version of the work is not maintained on source or the source's work is newer than agent, send
		// the newer work to agent
		if currentResourceVersion == 0 || currentResourceVersion > lastResourceVersion {
			if err := c.Publish(ctx, eventType, obj); err != nil {
				return err
			}
		}
	}

	// the resources do not exist on the source, but exist on the agent, delete them
	for _, rv := range resourceVersions.Versions {
		_, exists := getObj(rv.ResourceID, objs)
		if exists {
			continue
		}

		// send a delete event for the current resource
		evt := types.NewEventBuilder(c.sourceID, eventType).
			WithResourceID(rv.ResourceID).
			WithResourceVersion(rv.ResourceVersion).
			WithClusterName(fmt.Sprintf("%s", clusterName)).
			WithDeletionTimestamp(metav1.Now().Time).
			NewEvent()
		if err := c.publish(ctx, evt); err != nil {
			return err
		}
		increaseCloudEventsSentCounter(evt.Source(), "", fmt.Sprintf("%s", clusterName), evtDataType.String(), string(eventType.SubResource), string(eventType.Action))
	}

	return nil
}

func findResourceVersion(id string, versions []payload.ResourceVersion) int64 {
	for _, version := range versions {
		if id == version.ResourceID {
			return version.ResourceVersion
		}
	}

	return 0
}
