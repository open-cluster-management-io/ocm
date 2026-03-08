package metrics

import (
	"sync"

	"google.golang.org/grpc"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"

	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"

	cemetrics "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/metrics"
)

// ensure metrics are registered only once
var once sync.Once

// subsystem used to define the metrics for grpc server
const grpcMetricsSubsystem = "grpc_server"

// Names of the labels added to metrics:
const (
	grpcMetricsMethodLabel     = "grpc_method"
	grpcMetricsServiceLabel    = "grpc_service"
	grpcMetricsTypeLabel       = "grpc_type"
	grpcMetricsCodeLabel       = "grpc_code"
	grpcMetricsRemoteAddrLabel = "remote_addr"
	grpcMetricsLocalAddrLabel  = "local_addr"
)

// grpcMetricsLabels - Array of labels added to grpc server metrics:
var grpcMetricsLabels = []string{
	grpcMetricsMethodLabel,
	grpcMetricsServiceLabel,
	grpcMetricsTypeLabel,
}

// grpcConnMetricsLabels - Array of labels added to grpc server connection metrics:
var grpcConnMetricsLabels = []string{
	grpcMetricsRemoteAddrLabel,
	grpcMetricsLocalAddrLabel,
}

// Names of the grpc server metrics:
const (
	activeConnectionsMetric = "active_connections"
	msgRevBytesCountMetric  = "msg_received_bytes_total"
	msgSentBytesCountMetric = "msg_sent_bytes_total"
)

// grpcServerConnections is a gauge metric that tracks the number of
// active gRPC server connections.
var grpcServerConnections = k8smetrics.NewGaugeVec(&k8smetrics.GaugeOpts{
	Subsystem:      grpcMetricsSubsystem,
	Name:           activeConnectionsMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Current number of active gRPC server connections.",
}, grpcConnMetricsLabels)

// grpcServerMsgRevBytes is a counter metric that tracks the total number of
// bytes received on the gRPC server.
var grpcServerMsgRevBytes = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
	Subsystem:      grpcMetricsSubsystem,
	Name:           msgRevBytesCountMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Total number of bytes received on the gRPC server.",
}, grpcMetricsLabels)

// grpcServerMsgSentBytes is a counter metric that tracks the total number of
// bytes sent by the gRPC server.
var grpcServerMsgSentBytes = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
	Subsystem:      grpcMetricsSubsystem,
	Name:           msgSentBytesCountMetric,
	StabilityLevel: k8smetrics.ALPHA,
	Help:           "Total number of bytes sent by the gRPC server.",
}, grpcMetricsLabels)

// NewGRPCMetricsUnaryInterceptor creates a chained unary interceptor for server metrics that
// include grpc prometheus server metrics and cloud events metrics.
func NewGRPCMetricsUnaryInterceptor(promServerMetrics *prom.ServerMetrics) grpc.UnaryServerInterceptor {
	return middleware.ChainUnaryServer(
		promServerMetrics.UnaryServerInterceptor(),
		cemetrics.NewCloudEventsMetricsUnaryInterceptor(),
	)
}

// NewGRPCMetricsStreamInterceptor creates a chained stream interceptor for server metrics that
// include grpc prometheus server metrics and cloud events metrics.
func NewGRPCMetricsStreamInterceptor(promServerMetrics *prom.ServerMetrics) grpc.StreamServerInterceptor {
	return middleware.ChainStreamServer(
		promServerMetrics.StreamServerInterceptor(),
		cemetrics.NewCloudEventsMetricsStreamInterceptor(),
	)
}

// Register all the grpc server metrics.
func RegisterGRPCMetrics(promServerMetrics *prom.ServerMetrics, extraMetrics ...k8smetrics.Registerable) {
	once.Do(func() {
		metrics := make([]k8smetrics.Registerable, 0, 3+len(extraMetrics))
		metrics = append(metrics,
			grpcServerConnections,
			grpcServerMsgRevBytes,
			grpcServerMsgSentBytes,
		)

		metrics = append(metrics, extraMetrics...)

		for _, m := range metrics {
			if m != nil {
				legacyregistry.MustRegister(m)
			} else {
				klog.Errorf("failed to register nil grpc server metric")
			}
		}
		legacyregistry.RawMustRegister(promServerMetrics)
	})
}
