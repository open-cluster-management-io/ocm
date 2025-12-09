package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/constants"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/heartbeat"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/metrics"

	"k8s.io/apimachinery/pkg/api/errors"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	grpcprotocol "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

type resourceHandler func(ctx context.Context, subID string, res *cloudevents.Event) error

// subscriber defines a subscriber that can receive and handle resource spec.
type subscriber struct {
	clusterName string
	dataType    types.CloudEventsDataType
	handler     resourceHandler
}

var _ server.AgentEventServer = &GRPCBroker{}

// GRPCBroker is a gRPC broker that implements the CloudEventServiceServer interface.
// It broadcasts resource spec to agents and listens for resource status updates from them.
type GRPCBroker struct {
	pbv1.UnimplementedCloudEventServiceServer
	services               map[types.CloudEventsDataType]server.Service
	subscribers            map[string]*subscriber // registered subscribers
	heartbeatCheckInterval time.Duration
	mu                     sync.RWMutex
}

// NewGRPCBroker creates a new gRPC broker with the given gRPC server.
func NewGRPCBroker() *GRPCBroker {
	broker := &GRPCBroker{
		subscribers:            make(map[string]*subscriber),
		services:               make(map[types.CloudEventsDataType]server.Service),
		heartbeatCheckInterval: 10 * time.Second,
	}
	return broker
}

func (bkr *GRPCBroker) RegisterService(ctx context.Context, t types.CloudEventsDataType, service server.Service) {
	bkr.services[t] = service
	service.RegisterHandler(ctx, bkr)
}

func (bkr *GRPCBroker) Subscribers() sets.Set[string] {
	bkr.mu.Lock()
	defer bkr.mu.Unlock()

	subscribers := sets.New[string]()
	for _, sub := range bkr.subscribers {
		subscribers.Insert(sub.clusterName)
	}

	return subscribers
}

// Publish in stub implementation for agent publish resource status.
func (bkr *GRPCBroker) Publish(ctx context.Context, pubReq *pbv1.PublishRequest) (*emptypb.Empty, error) {
	logger := klog.FromContext(ctx)
	// WARNING: don't use "evt, err := pb.FromProto(pubReq.Event)" to convert protobuf to cloudevent
	evt, err := binding.ToEvent(ctx, grpcprotocol.NewMessage(pubReq.Event))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("failed to convert protobuf to cloudevent: %v", err))
	}

	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("failed to parse cloud event type %s, %v", evt.Type(), err))
	}

	logger.V(4).Info("receive the event with grpc broker", "eventType", evt.Type(), "extensions", evt.Extensions())

	// handler resync request
	if eventType.Action == types.ResyncRequestAction {
		err := bkr.respondResyncSpecRequest(ctx, eventType.CloudEventsDataType, evt)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("failed to respond resync spec request: %v", err))
		}
		return &emptypb.Empty{}, nil
	}

	service, ok := bkr.services[eventType.CloudEventsDataType]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("failed to find service for event type %s", eventType.CloudEventsDataType))
	}

	// handle the resource status update according status update type
	if err := service.HandleStatusUpdate(ctx, evt); err != nil {
		errStr, marshalErr := json.Marshal(err)
		if marshalErr != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}

		return nil, status.Error(codes.FailedPrecondition, string(errStr))
	}

	return &emptypb.Empty{}, nil
}

// register registers a subscriber and return client id and error channel.
func (bkr *GRPCBroker) register(ctx context.Context,
	dataType types.CloudEventsDataType,
	subReq *pbv1.SubscriptionRequest,
	subServer pbv1.CloudEventService_SubscribeServer,
	handler resourceHandler) (string, error) {
	logger := klog.FromContext(ctx)

	bkr.mu.Lock()
	defer bkr.mu.Unlock()

	id := uuid.NewString()
	bkr.subscribers[id] = &subscriber{
		clusterName: subReq.ClusterName,
		dataType:    dataType,
		handler:     handler,
	}

	// Signal subscriber is registered
	if err := subServer.SendHeader(metadata.Pairs(constants.GRPCSubscriptionIDKey, id)); err != nil {
		logger.Error(err, "failed to send subscription header, unregister subscriber", "subID", id)
		delete(bkr.subscribers, id)
		return "", err
	}
	logger.V(4).Info("register a subscriber", "id", id, "clusterName", subReq.ClusterName, "dataType", dataType)
	metrics.IncGRPCCESubscribersMetric(subReq.ClusterName, dataType.String())

	return id, nil
}

// unregister a subscriber by id
func (bkr *GRPCBroker) unregister(ctx context.Context, id string) {
	bkr.mu.Lock()
	defer bkr.mu.Unlock()

	logger := klog.FromContext(ctx)
	if sub, exists := bkr.subscribers[id]; exists {
		logger.V(4).Info("unregister subscriber", "id", id, "clusterName", sub.clusterName, "dataType", sub.dataType)
		delete(bkr.subscribers, id)
		metrics.DecGRPCCESubscribersMetric(sub.clusterName, sub.dataType.String())
	}
}

// Subscribe in stub implementation for agent subscribe resource spec.
// Note: It's unnecessary to send a status resync request to agent subscribers.
// The agent will continuously attempt to send status updates to the gRPC broker.
// If the broker is down or disconnected, the agent will resend the status once the broker is back up or reconnected.
func (bkr *GRPCBroker) Subscribe(subReq *pbv1.SubscriptionRequest, subServer pbv1.CloudEventService_SubscribeServer) error {
	if len(subReq.ClusterName) == 0 {
		return fmt.Errorf("invalid subscription request: missing cluster name")
	}
	// register the cluster for subscription to the resource spec
	dataType, err := types.ParseCloudEventsDataType(subReq.DataType)
	if err != nil {
		return fmt.Errorf("invalid subscription request: invalid data type %v", err)
	}

	subCtx, cancel := context.WithCancel(subServer.Context())
	defer cancel()

	logger := klog.FromContext(subCtx).WithValues("clusterName", subReq.ClusterName)

	// TODO make the channel size configurable
	eventCh := make(chan *pbv1.CloudEvent, 100)

	heartbeater := heartbeat.NewHeartbeater(bkr.heartbeatCheckInterval, 10)
	sendErrCh := make(chan error, 1)

	// send events
	// The grpc send is not concurrency safe and non-blocking, see: https://github.com/grpc/grpc-go/blob/v1.75.1/stream.go#L1571
	// Return the error without wrapping, as it includes the gRPC error code and message for further handling.
	// For unrecoverable errors, such as a connection closed by an intermediate proxy, push the error to subscriber's
	// error channel to unregister the subscriber.
	go func() {
		for {
			select {
			case <-subCtx.Done():
				return
			case evt := <-heartbeater.Heartbeat():
				if err := subServer.Send(evt); err != nil {
					logger.Error(err, "failed to send heartbeat")
					// Unblock producers (handler select) and exit heartbeat ticker.
					cancel()
					select {
					case sendErrCh <- err:
					default:
					}
					return
				}
			case evt := <-eventCh:
				if err := subServer.Send(evt); err != nil {
					logger.Error(err, "failed to send event")
					// Unblock producers (handler select) and exit heartbeat ticker.
					cancel()
					select {
					case sendErrCh <- err:
					default:
					}
					return
				}
			}
		}
	}()

	subID, err := bkr.register(klog.NewContext(subCtx, logger), *dataType, subReq, subServer, func(handlerCtx context.Context, subID string, evt *cloudevents.Event) error {
		// convert the cloudevents.Event to pbv1.CloudEvent
		// WARNING: don't use "pbEvt, err := pb.ToProto(evt)" to convert cloudevent to protobuf
		pbEvt := &pbv1.CloudEvent{}
		if err := grpcprotocol.WritePBMessage(handlerCtx, binding.ToMessage(evt), pbEvt); err != nil {
			return fmt.Errorf("failed to convert cloudevent to protobuf for resource(%s): %v", evt.ID(), err)
		}

		// send the cloudevent to the subscriber
		logger.V(4).Info("sending the event to spec subscribers",
			"subID", subID, "eventType", evt.Type(), "extensions", evt.Extensions())
		select {
		case eventCh <- pbEvt:
		case <-subCtx.Done():
			// The context of the stream has been canceled or completed.
			// This could happen if:
			// - The client closed the connection or canceled the stream.
			// - The server closed the stream, potentially due to a shutdown.
			// No error is returned here because the stream closure is expected.
			return nil
		}

		return nil
	})
	if err != nil {
		return err
	}

	go heartbeater.Start(subCtx)

	select {
	case err := <-sendErrCh:
		logger.Error(err, "failed to send event, unregister subscriber", "subID", subID)
		bkr.unregister(subCtx, subID)
		return err
	case <-subCtx.Done():
		// The context of the stream has been canceled or completed.
		// This could happen if:
		// - The client closed the connection or canceled the stream.
		// - The server closed the stream, potentially due to a shutdown.
		// Regardless of the reason, unregister the subscriber and stop processing.
		// No error is returned here because the stream closure is expected.
		bkr.unregister(subCtx, subID)
		return nil
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
func (bkr *GRPCBroker) respondResyncSpecRequest(ctx context.Context, eventDataType types.CloudEventsDataType, evt *cloudevents.Event) error {
	log := klog.FromContext(ctx).WithValues(
		"eventDataType", eventDataType, "eventType", evt.Type(), "extensions", evt.Extensions())

	resourceVersions, err := payload.DecodeSpecResyncRequest(*evt)
	if err != nil {
		return err
	}

	clusterNameValue, err := evt.Context.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return err
	}
	clusterName := fmt.Sprintf("%s", clusterNameValue)

	service, ok := bkr.services[eventDataType]
	if !ok {
		return fmt.Errorf("failed to find service for event type %s", eventDataType)
	}

	objs, err := service.List(types.ListOptions{ClusterName: clusterName, CloudEventsDataType: eventDataType})
	if err != nil {
		return err
	}

	if len(objs) == 0 {
		log.V(4).Info("no objs from the lister, do nothing")
		return nil
	}

	for _, obj := range objs {
		// respond with the deleting resource regardless of the resource version
		objLogger := log.WithValues("eventType", obj.Type(), "extensions", obj.Extensions())
		if _, ok := obj.Extensions()[types.ExtensionDeletionTimestamp]; ok {
			objLogger.V(4).Info("respond spec resync request")
			err = bkr.handleRes(ctx, obj, eventDataType, "delete_request")
			if err != nil {
				objLogger.Error(err, "failed to handle resync spec request")
			}
			continue
		}

		lastResourceVersion := findResourceVersion(obj.ID(), resourceVersions.Versions)
		currentResourceVersion, err := cloudeventstypes.ToInteger(obj.Extensions()[types.ExtensionResourceVersion])
		if err != nil {
			objLogger.V(4).Info("ignore the event since it has a invalid resourceVersion", "error", err)
			continue
		}

		// the version of the work is not maintained on source or the source's work is newer than agent, send
		// the newer work to agent
		if currentResourceVersion == 0 || int64(currentResourceVersion) > lastResourceVersion {
			objLogger.V(4).Info("respond spec resync request")
			err := bkr.handleRes(ctx, obj, eventDataType, "update_request")
			if err != nil {
				objLogger.Error(err, "failed to handle resync spec request")
			}
		}
	}

	// the resources do not exist on the source, but exist on the agent, delete them
	for _, rv := range resourceVersions.Versions {
		_, exists := getObj(rv.ResourceID, objs)
		if exists {
			continue
		}

		deleteEventTypes := types.CloudEventsType{
			CloudEventsDataType: eventDataType,
			SubResource:         types.SubResourceSpec,
		}
		obj := types.NewEventBuilder("source", deleteEventTypes).
			WithResourceID(rv.ResourceID).
			WithResourceVersion(rv.ResourceVersion).
			WithClusterName(clusterName).
			WithDeletionTimestamp(time.Now()).
			NewEvent()

		// send a delete event for the current resource
		log.V(4).Info("respond spec resync request")
		err := bkr.handleRes(ctx, &obj, eventDataType, "delete_request")
		if err != nil {
			log.Error(err, "failed to handle delete request")
		}
	}

	return nil
}

// handleRes publish the resource to the correct subscriber.
func (bkr *GRPCBroker) handleRes(
	ctx context.Context,
	evt *cloudevents.Event,
	t types.CloudEventsDataType,
	action types.EventAction) error {

	bkr.mu.RLock()
	defer bkr.mu.RUnlock()

	eventType := types.CloudEventsType{
		CloudEventsDataType: t,
		SubResource:         types.SubResourceSpec,
		Action:              action,
	}
	evt.SetType(eventType.String())

	clusterNameValue, err := evt.Context.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return err
	}
	clusterName := fmt.Sprintf("%s", clusterNameValue)

	for subID, subscriber := range bkr.subscribers {
		// checks if the event should be processed by the current instance by verifying
		// the resource consumer name and its data type is in the subscriber list, ensuring
		// the event will be only processed when the consumer is subscribed to the current
		// broker.
		if subscriber.clusterName == clusterName && subscriber.dataType == t {
			if err := subscriber.handler(ctx, subID, evt); err != nil {
				return err
			}
		}
	}
	return nil
}

// OnCreate is called by the controller when a resource is created on the maestro server.
func (bkr *GRPCBroker) OnCreate(ctx context.Context, t types.CloudEventsDataType, id string) error {
	service, ok := bkr.services[t]
	if !ok {
		return fmt.Errorf("failed to find service for event type %s", t)
	}

	resource, err := service.Get(ctx, id)
	// if the resource is not found, it indicates the resource has been processed.
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return bkr.handleRes(ctx, resource, t, "create_request")
}

// OnUpdate is called by the controller when a resource is updated on the maestro server.
func (bkr *GRPCBroker) OnUpdate(ctx context.Context, t types.CloudEventsDataType, id string) error {
	service, ok := bkr.services[t]
	if !ok {
		return fmt.Errorf("failed to find service for event type %s", t)
	}

	resource, err := service.Get(ctx, id)
	// if the resource is not found, it indicates the resource has been processed.
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return bkr.handleRes(ctx, resource, t, "update_request")
}

// OnDelete is called by the controller when a resource is deleted from the maestro server.
func (bkr *GRPCBroker) OnDelete(ctx context.Context, t types.CloudEventsDataType, id string) error {
	service, ok := bkr.services[t]
	if !ok {
		return fmt.Errorf("failed to find service for event type %s", t)
	}

	resource, err := service.Get(ctx, id)
	// if the resource is not found, it indicates the resource has been processed.
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return bkr.handleRes(ctx, resource, t, "delete_request")
}

// IsConsumerSubscribed returns true if the consumer is subscribed to the broker for resource spec.
func (bkr *GRPCBroker) IsConsumerSubscribed(consumerName string) bool {
	bkr.mu.RLock()
	defer bkr.mu.RUnlock()
	for _, subscriber := range bkr.subscribers {
		if subscriber.clusterName == consumerName {
			return true
		}
	}
	return false
}

// findResourceVersion returns the resource version for the given ID from the list of resource versions.
func findResourceVersion(id string, versions []payload.ResourceVersion) int64 {
	for _, version := range versions {
		if id == version.ResourceID {
			return version.ResourceVersion
		}
	}

	return 0
}

// getObj returns the object with the given ID from the list of resources.
func getObj(id string, objs []*cloudevents.Event) (*cloudevents.Event, bool) {
	for _, obj := range objs {
		resID := obj.Extensions()[types.ExtensionResourceID]
		resIDStr, ok := resID.(string)
		if ok && id == resIDStr {
			return obj, true
		}
	}

	return nil, false
}
