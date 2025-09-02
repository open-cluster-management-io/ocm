package metrics

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/cloudevents/sdk-go/v2/binding"
	cetypes "github.com/cloudevents/sdk-go/v2/types"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	k8smetrics "k8s.io/component-base/metrics"
)

// subsystem used to define the metrics of grpc server for cloud events
const grpcCEMetricsSubsystem = "grpc_server_ce"

// Names of the labels added to metrics:
const (
	grpcCEMetricsClusterLabel  = "cluster"
	grpcCEMetricsDataTypeLabel = "data_type"
	grpcCEMetricsMethodLabel   = "method"
	grpcMetricsCodeLabel       = "grpc_code"
)

// grpcCEMetricsLabels - Array of labels added to grpc server metrics for cloudevents:
var grpcCEMetricsLabels = []string{
	grpcCEMetricsClusterLabel,
	grpcCEMetricsDataTypeLabel,
	grpcCEMetricsMethodLabel,
}

// grpcCEMetricsAllLabels - Array of all labels added to grpc server metrics for cloudevents:
var grpcCEMetricsAllLabels = append(grpcCEMetricsLabels, grpcMetricsCodeLabel)

// Names of the grpc server metrics for cloudevents:
const (
	calledCountMetric          = "called_total"
	processedCountMetric       = "processed_total"
	processingDurationMetric   = "processing_duration_seconds"
	messageReceivedCountMetric = "msg_received_total"
	messageSentCountMetric     = "msg_sent_total"
)

// grpcCECalledCountMetric is a counter metric that tracks the total number of
// RPC requests for cloudevents called on the gRPC server.
var grpcCECalledCountMetric = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
	Subsystem:      grpcCEMetricsSubsystem,
	Name:           calledCountMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Total number of RPC requests for cloudevents called on the grpc server.",
}, grpcCEMetricsLabels)

// grpcCEMessageReceivedCountMetric is a counter metric that tracks the total number of
// messages for cloudevents received on the gRPC server.
var grpcCEMessageReceivedCountMetric = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
	Subsystem:      grpcCEMetricsSubsystem,
	Name:           messageReceivedCountMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Total number of messages for cloudevents received on the gRPC server.",
}, grpcCEMetricsLabels)

// grpcCEMessageSentCountMetric is a counter metric that tracks the total number of
// messages for cloudevents sent by the gRPC server.
var grpcCEMessageSentCountMetric = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
	Subsystem:      grpcCEMetricsSubsystem,
	Name:           messageSentCountMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Total number of messages for cloudevents sent by the gRPC server.",
}, grpcCEMetricsLabels)

// grpcCEProcessedCountMetric is a counter metric that tracks the total number of
// RPC requests for cloudevents processed on the server, regardless of success or failure.
var grpcCEProcessedCountMetric = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
	Subsystem:      grpcCEMetricsSubsystem,
	Name:           processedCountMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Total number of RPC requests for cloudevents processed on the server, regardless of success or failure.",
}, grpcCEMetricsAllLabels,
)

// grpcCEProcessingDurationMetric is a histogram metric that tracks the duration of
// RPC requests for cloudevents processed on the server.
var grpcCEProcessingDurationMetric = k8smetrics.NewHistogramVec(&k8smetrics.HistogramOpts{
	Subsystem:      grpcCEMetricsSubsystem,
	Name:           processingDurationMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Histogram of the duration of RPC requests for cloudevents processed on the server.",
	Buckets:        k8smetrics.ExponentialBuckets(10e-7, 10, 10),
}, grpcCEMetricsAllLabels)

const gRPCCloudEventService = "io.cloudevents.v1.CloudEventService"

// NewCloudEventsMetricsUnaryInterceptor creates a unary server interceptor for cloudevents metrics.
func NewCloudEventsMetricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		startTime := time.Now()

		// extract the service and method
		service, method := SplitMethod(info.FullMethod)
		if service != gRPCCloudEventService {
			// skip emitting cloudevents metrics if it's not a cloudevents RPC request
			return handler(ctx, req)
		}

		// initialize defaults for error cases
		cluster := "unknown"
		dataType := "unknown"

		pubReq, ok := req.(*pbv1.PublishRequest)
		if !ok {
			err := fmt.Errorf("invalid request type for Publish method")
			recordCloudEventsMetrics(cluster, dataType, method, err, startTime)
			return nil, err
		}
		// convert the request to cloudevent and extract the source
		evt, err := binding.ToEvent(ctx, protocol.NewMessage(pubReq.Event))
		if err != nil {
			err = fmt.Errorf("failed to convert to cloudevent: %v", err)
			recordCloudEventsMetrics(cluster, dataType, method, err, startTime)
			return nil, err
		}

		// extract the cluster name from event extensions
		clusterVal, err := cetypes.ToString(evt.Context.GetExtensions()[types.ExtensionClusterName])
		if err != nil {
			err = fmt.Errorf("failed to get clustername extension: %v", err)
			recordCloudEventsMetrics(cluster, dataType, method, err, startTime)
			return nil, err
		}
		cluster = clusterVal

		// extract the data type from event type
		eventType, err := types.ParseCloudEventsType(evt.Type())
		if err != nil {
			err = fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
			recordCloudEventsMetrics(cluster, dataType, method, err, startTime)
			return nil, err
		}
		dataType = eventType.CloudEventsDataType.String()

		grpcCECalledCountMetric.WithLabelValues(cluster, dataType, method).Inc()
		grpcCEMessageReceivedCountMetric.WithLabelValues(cluster, dataType, method).Inc()
		// call rpc handler to handle RPC request
		resp, err := handler(ctx, req)
		duration := time.Since(startTime).Seconds()
		grpcCEMessageSentCountMetric.WithLabelValues(cluster, dataType, method).Inc()

		// get status code from error
		status := statusFromError(err)
		code := status.Code()
		grpcCEProcessedCountMetric.WithLabelValues(cluster, dataType, method, code.String()).Inc()
		grpcCEProcessingDurationMetric.WithLabelValues(cluster, dataType, method, code.String()).Observe(duration)

		return resp, err
	}
}

func recordCloudEventsMetrics(cluster, dataType, method string, err error, startTime time.Time) {
	duration := time.Since(startTime).Seconds()
	status := statusFromError(err)
	code := status.Code()
	grpcCEProcessedCountMetric.WithLabelValues(cluster, dataType, method, code.String()).Inc()
	grpcCEProcessingDurationMetric.WithLabelValues(cluster, dataType, method, code.String()).Observe(duration)
}

// wrappedCloudEventsMetricsStream wraps a grpc.ServerStream, capturing the request source
// emitting metrics for the stream interceptor.
type wrappedCloudEventsMetricsStream struct {
	clusterName *string
	dataType    *string
	method      string
	grpc.ServerStream
	ctx context.Context
}

// RecvMsg wraps the RecvMsg method of the embedded grpc.ServerStream.
// It captures the cluster and data type from the SubscriptionRequest and emits metrics.
func (w *wrappedCloudEventsMetricsStream) RecvMsg(m interface{}) error {
	err := w.ServerStream.RecvMsg(m)
	if err != nil {
		return err
	}

	subReq, ok := m.(*pbv1.SubscriptionRequest)
	if !ok {
		return fmt.Errorf("invalid request type for Subscribe method")
	}

	if w.clusterName != nil && w.dataType != nil {
		*w.clusterName = subReq.ClusterName
		*w.dataType = subReq.DataType
		grpcCECalledCountMetric.WithLabelValues(*w.clusterName, *w.dataType, w.method).Inc()
		grpcCEMessageReceivedCountMetric.WithLabelValues(*w.clusterName, *w.dataType, w.method).Inc()
	}

	return nil
}

// SendMsg wraps the SendMsg method of the embedded grpc.ServerStream.
func (w *wrappedCloudEventsMetricsStream) SendMsg(m interface{}) error {
	err := w.ServerStream.SendMsg(m)
	if err != nil {
		return err
	}

	if w.clusterName != nil && w.dataType != nil && *w.clusterName != "" && *w.dataType != "" {
		grpcCEMessageSentCountMetric.WithLabelValues(*w.clusterName, *w.dataType, w.method).Inc()
	}

	return nil
}

// newWrappedCloudEventsMetricsStream creates a wrappedCloudEventsMetricsStream with the specified type and cluster reference.
func newWrappedCloudEventsMetricsStream(clusterName, dataType *string, method string, ctx context.Context, ss grpc.ServerStream) grpc.ServerStream {
	return &wrappedCloudEventsMetricsStream{clusterName, dataType, method, ss, ctx}
}

// NewCloudEventsMetricsStreamInterceptor creates a stream server interceptor for server metrics.
func NewCloudEventsMetricsStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// extract the service and method
		service, method := SplitMethod(info.FullMethod)
		if service != gRPCCloudEventService {
			// skip emitting cloudevents metrics if it's not a cloudevents RPC request
			return handler(srv, stream)
		}

		dataType := ""
		cluster := ""
		// create a wrapped stream to capture the source and emit metrics
		wrappedCEMetricsStream := newWrappedCloudEventsMetricsStream(&cluster, &dataType, method, stream.Context(), stream)
		// call rpc handler to handle RPC request
		err := handler(srv, wrappedCEMetricsStream)

		// get status code from error
		status := statusFromError(err)
		code := status.Code()
		grpcCEProcessedCountMetric.WithLabelValues(cluster, dataType, method, code.String()).Inc()

		return err
	}
}

// statusFromError returns a grpc status. If the error code is neither a valid grpc status
// nor a context error, codes.Unknown will be set.
func statusFromError(err error) *status.Status {
	s, ok := status.FromError(err)
	// mirror what the grpc server itself does, i.e. also convert context errors to status
	if !ok {
		s = status.FromContextError(err)
	}
	return s
}

// SplitMethod parses grpc full method "/package.service/method" to service and method
func SplitMethod(fullMethod string) (service, method string) {
	if fullMethod == "" {
		return "unknown", "unknown"
	}
	// remove leading "/"
	if fullMethod[0] == '/' {
		fullMethod = fullMethod[1:]
	}
	// split at last "/"
	for i := len(fullMethod) - 1; i >= 0; i-- {
		if fullMethod[i] == '/' {
			return fullMethod[:i], fullMethod[i+1:]
		}
	}

	return fullMethod, "unknown"
}

// Register all the grpc server metrics for cloudevents.
func CloudEventsGRPCMetrics() []k8smetrics.Registerable {
	return []k8smetrics.Registerable{
		grpcCECalledCountMetric,
		grpcCEProcessedCountMetric,
		grpcCEProcessingDurationMetric,
		grpcCEMessageReceivedCountMetric,
		grpcCEMessageSentCountMetric,
	}
}
