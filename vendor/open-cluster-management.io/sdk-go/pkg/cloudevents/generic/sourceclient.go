package generic

import (
	"context"
	"fmt"
	"strconv"

	cloudevents "github.com/cloudevents/sdk-go/v2"

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
	codecs           map[types.CloudEventsDataType]Codec[T]
	statusHashGetter StatusHashGetter[T]
	sourceID         string
}

// NewCloudEventSourceClient returns an instance for CloudEventSourceClient. The following arguments are required to
// create a client
//   - sourceOptions provides the sourceID and the cloudevents clients that are based on different event protocols for
//     sending/receiving the cloudevents.
//   - lister gets the resources from a cache/store of a source.
//   - statusHashGetter calculates the resource status hash.
//   - codecs is list of codecs for encoding/decoding a resource objet/cloudevent to/from a cloudevent/resource objet.
func NewCloudEventSourceClient[T ResourceObject](
	ctx context.Context,
	sourceOptions *options.CloudEventsSourceOptions,
	lister Lister[T],
	statusHashGetter StatusHashGetter[T],
	codecs ...Codec[T],
) (*CloudEventSourceClient[T], error) {
	baseClient := &baseClient{
		cloudEventsOptions:     sourceOptions.CloudEventsOptions,
		cloudEventsRateLimiter: NewRateLimiter(sourceOptions.EventRateLimit),
	}

	if err := baseClient.connect(ctx); err != nil {
		return nil, err
	}

	evtCodes := make(map[types.CloudEventsDataType]Codec[T])
	for _, codec := range codecs {
		evtCodes[codec.EventDataType()] = codec
	}

	return &CloudEventSourceClient[T]{
		baseClient:       baseClient,
		lister:           lister,
		codecs:           evtCodes,
		statusHashGetter: statusHashGetter,
		sourceID:         sourceOptions.SourceID,
	}, nil
}

// Resync the resources status by sending a status resync request from a source to clusters with list options.
func (c *CloudEventSourceClient[T]) Resync(ctx context.Context, listOptions types.ListOptions) error {
	// list the resource objects that are maintained by the current source with list options
	objs, err := c.lister.List(listOptions)
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

	// only resync the resources whose event data type is registered
	for eventDataType := range c.codecs {
		eventType := types.CloudEventsType{
			CloudEventsDataType: eventDataType,
			SubResource:         types.SubResourceStatus,
			Action:              types.ResyncRequestAction,
		}

		evt := types.NewEventBuilder(c.sourceID, eventType).NewEvent()
		if err := evt.SetData(cloudevents.ApplicationJSON, hashes); err != nil {
			return fmt.Errorf("failed to set data to cloud event: %v", err)
		}

		if err := c.publish(ctx, evt); err != nil {
			return err
		}
	}

	return nil
}

// Publish a resource spec from a source to an agent.
func (c *CloudEventSourceClient[T]) Publish(ctx context.Context, eventType types.CloudEventsType, obj T) error {
	if eventType.SubResource != types.SubResourceSpec {
		return fmt.Errorf("unsupported event eventType %s", eventType)
	}

	codec, ok := c.codecs[eventType.CloudEventsDataType]
	if !ok {
		return fmt.Errorf("failed to find the codec for event %s", eventType.CloudEventsDataType)
	}

	evt, err := codec.Encode(c.sourceID, eventType, obj)
	if err != nil {
		return err
	}

	if err := c.publish(ctx, *evt); err != nil {
		return err
	}

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
	klog.V(4).Infof("Received event:\n%s", evt)

	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		klog.Errorf("failed to parse cloud event type, %v", err)
		return
	}

	if eventType.Action == types.ResyncRequestAction {
		if eventType.SubResource != types.SubResourceSpec {
			klog.Warningf("unsupported event type %s, ignore", eventType)
			return
		}

		if err := c.respondResyncSpecRequest(ctx, eventType.CloudEventsDataType, evt); err != nil {
			klog.Errorf("failed to resync resources spec, %v", err)
		}

		return
	}

	codec, ok := c.codecs[eventType.CloudEventsDataType]
	if !ok {
		klog.Warningf("failed to find the codec for event %s, ignore", eventType.CloudEventsDataType)
		return
	}

	if eventType.SubResource != types.SubResourceStatus {
		klog.Warningf("unsupported event type %s, ignore", eventType)
		return
	}

	clusterName, err := evt.Context.GetExtension(types.ExtensionClusterName)
	if err != nil {
		klog.Errorf("failed to find cluster name, %v", err)
		return
	}

	obj, err := codec.Decode(&evt)
	if err != nil {
		klog.Errorf("failed to decode status, %v", err)
		return
	}

	action, err := c.statusAction(fmt.Sprintf("%s", clusterName), obj)
	if err != nil {
		klog.Errorf("failed to generate status event %s, %v", evt, err)
		return
	}

	if len(action) == 0 {
		// no action is required, ignore
		return
	}

	for _, handler := range handlers {
		if err := handler(action, obj); err != nil {
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
	ctx context.Context, evtDataType types.CloudEventsDataType, evt cloudevents.Event) error {
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

	objs, err := c.lister.List(types.ListOptions{ClusterName: fmt.Sprintf("%s", clusterName), Source: c.sourceID})
	if err != nil {
		return err
	}

	for _, obj := range objs {
		lastResourceVersion := findResourceVersion(string(obj.GetUID()), resourceVersions.Versions)
		currentResourceVersion, err := strconv.ParseInt(obj.GetResourceVersion(), 10, 64)
		if err != nil {
			continue
		}

		if currentResourceVersion > lastResourceVersion {
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
	}

	return nil
}

func (c *CloudEventSourceClient[T]) statusAction(clusterName string, obj T) (evt types.ResourceAction, err error) {
	objs, err := c.lister.List(types.ListOptions{ClusterName: clusterName, Source: c.sourceID})
	if err != nil {
		return evt, err
	}

	lastObj, exists := getObj(string(obj.GetUID()), objs)
	if !exists {
		return evt, nil
	}

	lastStatusHash, err := c.statusHashGetter(lastObj)
	if err != nil {
		klog.Warningf("failed to hash object %s status, %v", lastObj.GetUID(), err)
		return evt, err
	}

	currentStatusHash, err := c.statusHashGetter(obj)
	if err != nil {
		klog.Warningf("failed to hash object %s status, %v", obj.GetUID(), err)
		return evt, nil
	}

	if lastStatusHash == currentStatusHash {
		return evt, nil
	}

	return types.StatusModified, nil
}

func findResourceVersion(id string, versions []payload.ResourceVersion) int64 {
	for _, version := range versions {
		if id == version.ResourceID {
			return version.ResourceVersion
		}
	}

	return 0
}
