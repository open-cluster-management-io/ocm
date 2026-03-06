package metrics

import (
	"context"
	"sync"

	"google.golang.org/grpc/stats"

	cemetrics "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/metrics"
)

type grpcHandlerContextKey string

const (
	contextKeyFullMethod grpcHandlerContextKey = "fullMethod"
	contextKeyRemoteAddr grpcHandlerContextKey = "remoteAddr"
	contextKeyLocalAddr  grpcHandlerContextKey = "localAddr"
)

var _ stats.Handler = &grpcMetricsHandler{}

// grpcMetricsHandler implements the stats.Handler interface to collect gRPC metrics
type grpcMetricsHandler struct {
	grpcTypes sync.Map // key: fullMethod, value: grpcType string
}

// NewGRPCMetricsHandler creates a new instance of grpcMetricsHandler
func NewGRPCMetricsHandler() *grpcMetricsHandler {
	return &grpcMetricsHandler{}
}

// TagRPC can attach gRPC full method to the given context
func (h *grpcMetricsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	ctx = context.WithValue(ctx, contextKeyFullMethod, info.FullMethodName)
	return ctx
}

// HandleRPC processes the RPC stats and records metrics.
func (h *grpcMetricsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	fullMethod, _ := ctx.Value(contextKeyFullMethod).(string)
	service, method := cemetrics.SplitMethod(fullMethod)

	switch st := s.(type) {
	case *stats.Begin:
		grpcType := "unary"
		if st.IsClientStream && st.IsServerStream {
			grpcType = "bidi_stream"
		} else if st.IsClientStream {
			grpcType = "client_stream"
		} else if st.IsServerStream {
			grpcType = "server_stream"
		}
		h.grpcTypes.Store(fullMethod, grpcType)
	case *stats.InPayload:
		val, _ := h.grpcTypes.Load(fullMethod)
		grpcType, _ := val.(string)
		if grpcType == "" {
			grpcType = "unknown"
		}
		grpcServerMsgRevBytes.WithLabelValues(method, service, grpcType).Add(float64(st.Length))
	case *stats.OutPayload:
		val, _ := h.grpcTypes.Load(fullMethod)
		grpcType, _ := val.(string)
		if grpcType == "" {
			grpcType = "unknown"
		}
		grpcServerMsgSentBytes.WithLabelValues(method, service, grpcType).Add(float64(st.Length))
	}
}

// TagConn can attach connection remote and local address to the given context
func (h *grpcMetricsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	if info.RemoteAddr != nil {
		ctx = context.WithValue(ctx, contextKeyRemoteAddr, info.RemoteAddr.String())
	}
	if info.LocalAddr != nil {
		ctx = context.WithValue(ctx, contextKeyLocalAddr, info.LocalAddr.String())
	}

	return ctx
}

// HandleConn processes the Conn stats and records connection metrics
func (h *grpcMetricsHandler) HandleConn(ctx context.Context, s stats.ConnStats) {
	remoteAddr, _ := ctx.Value(contextKeyRemoteAddr).(string)
	if remoteAddr == "" {
		remoteAddr = "unknown"
	}
	localAddr, _ := ctx.Value(contextKeyLocalAddr).(string)
	if localAddr == "" {
		localAddr = "unknown"
	}

	switch s.(type) {
	case *stats.ConnBegin:
		grpcServerConnections.WithLabelValues(remoteAddr, localAddr).Inc()
	case *stats.ConnEnd:
		grpcServerConnections.WithLabelValues(remoteAddr, localAddr).Dec()
	}
}
