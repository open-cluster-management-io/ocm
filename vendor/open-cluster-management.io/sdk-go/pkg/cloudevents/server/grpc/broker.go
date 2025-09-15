package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
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

type resourceHandler func(res *cloudevents.Event) error

// subscriber defines a subscriber that can receive and handle resource spec.
type subscriber struct {
	clusterName string
	dataType    types.CloudEventsDataType
	handler     resourceHandler
	errChan     chan<- error
}

var _ server.AgentEventServer = &GRPCBroker{}

// GRPCBroker is a gRPC broker that implements the CloudEventServiceServer interface.
// It broadcasts resource spec to agents and listens for resource status updates from them.
type GRPCBroker struct {
	pbv1.UnimplementedCloudEventServiceServer
	services    map[types.CloudEventsDataType]server.Service
	subscribers map[string]*subscriber // registered subscribers
	mu          sync.RWMutex
}

// NewGRPCBroker creates a new gRPC broker with the given gRPC server.
func NewGRPCBroker() *GRPCBroker {
	broker := &GRPCBroker{
		subscribers: make(map[string]*subscriber),
		services:    make(map[types.CloudEventsDataType]server.Service),
	}
	return broker
}

func (bkr *GRPCBroker) RegisterService(t types.CloudEventsDataType, service server.Service) {
	bkr.services[t] = service
	service.RegisterHandler(bkr)
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

	logger.V(4).Info("receive the event with grpc broker", "event", evt.Context)

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
func (bkr *GRPCBroker) register(
	clusterName string, dataType types.CloudEventsDataType, handler resourceHandler) (string, <-chan error) {
	bkr.mu.Lock()
	defer bkr.mu.Unlock()

	id := uuid.NewString()
	errChan := make(chan error)
	bkr.subscribers[id] = &subscriber{
		clusterName: clusterName,
		dataType:    dataType,
		handler:     handler,
		errChan:     errChan,
	}

	klog.V(4).Infof("register a subscriber %s (cluster name = %s)", id, clusterName)

	return id, errChan
}

// unregister a subscriber by id
func (bkr *GRPCBroker) unregister(id string) {
	bkr.mu.Lock()
	defer bkr.mu.Unlock()

	klog.V(10).Infof("unregister subscriber %s", id)
	if sub, exists := bkr.subscribers[id]; exists {
		close(sub.errChan)
		delete(bkr.subscribers, id)
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
	subscriberID, errChan := bkr.register(subReq.ClusterName, *dataType, func(evt *cloudevents.Event) error {
		// WARNING: don't use "pbEvt, err := pb.ToProto(evt)" to convert cloudevent to protobuf
		pbEvt := &pbv1.CloudEvent{}
		if err := grpcprotocol.WritePBMessage(context.TODO(), binding.ToMessage(evt), pbEvt); err != nil {
			// return the error to requeue the event if converting to protobuf fails (e.g., due to invalid cloudevent).
			return fmt.Errorf("failed to convert cloudevent to protobuf for resource(%s): %v", evt.ID(), err)
		}

		// send the cloudevent to the subscriber
		klog.V(4).Infof("sending the event to spec subscribers, %s", evt.Context)
		if err := subServer.Send(pbEvt); err != nil {
			klog.Errorf("failed to send grpc event, %v", err)
			// Return the error without wrapping, as it includes the gRPC error code and message for further handling.
			// For unrecoverable errors, such as a connection closed by an intermediate proxy, push the error to subscriber's
			// error channel to unregister the subscriber.
			return err
		}

		return nil
	})

	select {
	case err := <-errChan:
		// When reaching this point, an unrecoverable error occurred while sending the event,
		// such as the connection being closed. Unregister the subscriber to trigger agent reconnection.
		klog.Errorf("unregister subscriber %s, error= %v", subscriberID, err)
		bkr.unregister(subscriberID)
		return err
	case <-subServer.Context().Done():
		// The context of the stream has been canceled or completed.
		// This could happen if:
		// - The client closed the connection or canceled the stream.
		// - The server closed the stream, potentially due to a shutdown.
		// Regardless of the reason, unregister the subscriber and stop processing.
		// No error is returned here because the stream closure is expected.
		bkr.unregister(subscriberID)
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
	log := klog.FromContext(ctx)

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
		log.V(4).Info("there are is no objs from the list, do nothing")
		return nil
	}

	for _, obj := range objs {
		// respond with the deleting resource regardless of the resource version
		if _, ok := obj.Extensions()[types.ExtensionDeletionTimestamp]; ok {
			err = bkr.handleRes(ctx, obj, eventDataType, "delete_request")
			if err != nil {
				log.Error(err, "failed to handle resync spec request")
			}
			continue
		}

		lastResourceVersion := findResourceVersion(obj.ID(), resourceVersions.Versions)
		currentResourceVersion, err := cloudeventstypes.ToInteger(obj.Extensions()[types.ExtensionResourceVersion])
		if err != nil {
			log.V(4).Info("ignore the obj %v since it has a invalid resourceVersion, %v", obj, err)
			continue
		}

		// the version of the work is not maintained on source or the source's work is newer than agent, send
		// the newer work to agent
		if currentResourceVersion == 0 || int64(currentResourceVersion) > lastResourceVersion {
			err := bkr.handleRes(ctx, obj, eventDataType, "update_request")
			if err != nil {
				log.Error(err, "failed to handle resync spec request")
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
	log := klog.FromContext(ctx)

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

	// checks if the event should be processed by the current instance
	// by verifying the resource consumer name is in the subscriber list, ensuring the
	// event will be only processed when the consumer is subscribed to the current broker.
	if !bkr.IsConsumerSubscribed(clusterName) {
		log.V(4).Info("skip the event since the agent is not subscribed.")
		return nil
	}

	for _, subscriber := range bkr.subscribers {
		if subscriber.clusterName == clusterName && subscriber.dataType == t {
			if err := subscriber.handler(evt); err != nil {
				// check if the error is recoverable. For unrecoverable errors,
				// such as a connection closed by an intermediate proxy, push
				// the error to subscriber's error channel to unregister the subscriber.
				st, ok := status.FromError(err)
				if ok && st.Code() == codes.Unavailable {
					// TODO: handle more error codes that can't be recovered
					subscriber.errChan <- err
				}
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
