package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"k8s.io/apimachinery/pkg/util/errors"
	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authz"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/metrics"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type GRPCServer struct {
	options           *GRPCServerOptions
	extraMetrics      []k8smetrics.Registerable
	registerFuncs     []func(*grpc.Server)
	authenticators    []authn.Authenticator
	unaryAuthorizers  []authz.UnaryAuthorizer
	streamAuthorizers []authz.StreamAuthorizer
}

func NewGRPCServer(opt *GRPCServerOptions) *GRPCServer {
	return &GRPCServer{
		options: opt,
	}
}

func (b *GRPCServer) WithRegisterFunc(registerFunc func(*grpc.Server)) *GRPCServer {
	b.registerFuncs = append(b.registerFuncs, registerFunc)
	return b
}

func (b *GRPCServer) WithExtraMetrics(metrics ...k8smetrics.Registerable) *GRPCServer {
	b.extraMetrics = append(b.extraMetrics, metrics...)
	return b
}

func (b *GRPCServer) WithAuthenticator(authenticator authn.Authenticator) *GRPCServer {
	b.authenticators = append(b.authenticators, authenticator)
	return b
}

func (b *GRPCServer) WithUnaryAuthorizer(authorizer authz.UnaryAuthorizer) *GRPCServer {
	b.unaryAuthorizers = append(b.unaryAuthorizers, authorizer)
	return b
}

func (b *GRPCServer) WithStreamAuthorizer(authorizer authz.StreamAuthorizer) *GRPCServer {
	b.streamAuthorizers = append(b.streamAuthorizers, authorizer)
	return b
}

func (b *GRPCServer) Run(ctx context.Context) error {
	var grpcServerOptions []grpc.ServerOption
	grpcServerOptions = append(grpcServerOptions, grpc.MaxRecvMsgSize(b.options.MaxReceiveMessageSize))
	grpcServerOptions = append(grpcServerOptions, grpc.MaxSendMsgSize(b.options.MaxSendMessageSize))
	grpcServerOptions = append(grpcServerOptions, grpc.MaxConcurrentStreams(b.options.MaxConcurrentStreams))
	grpcServerOptions = append(grpcServerOptions, grpc.ConnectionTimeout(b.options.ConnectionTimeout))
	grpcServerOptions = append(grpcServerOptions, grpc.WriteBufferSize(b.options.WriteBufferSize))
	grpcServerOptions = append(grpcServerOptions, grpc.ReadBufferSize(b.options.ReadBufferSize))
	grpcServerOptions = append(grpcServerOptions, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             b.options.ClientMinPingInterval,
		PermitWithoutStream: b.options.PermitPingWithoutStream,
	}))
	grpcServerOptions = append(grpcServerOptions, grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionAge: b.options.MaxConnectionAge,
		Time:             b.options.ServerPingInterval,
		Timeout:          b.options.ServerPingTimeout,
	}))

	// Set textlogger with verbosity level 4 for controller-runtime logging
	log.SetLogger(textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))))
	// Serve with TLS - use certwatcher for dynamic certificate reloading
	certWatcher, err := certwatcher.New(b.options.TLSCertFile, b.options.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("failed to create certificate watcher: %v", err)
	}

	// Configure watch interval from options (default is 1 minute, configurable via --grpc-cert-watch-interval flag or YAML config)
	certWatcher.WithWatchInterval(b.options.CertWatchInterval)

	// This uses fsnotify for immediate detection + polling fallback
	go func() {
		if err := certWatcher.Start(ctx); err != nil {
			klog.FromContext(ctx).Error(err, "Certificate watcher stopped")
		}
	}()

	tlsConfig := &tls.Config{
		// Use GetCertificate callback from certwatcher
		// This allows dynamic certificate reloading on each TLS handshake
		GetCertificate: certWatcher.GetCertificate,
		MinVersion:     b.options.TLSMinVersion,
		MaxVersion:     b.options.TLSMaxVersion,
	}

	if b.options.ClientCAFile != "" {
		certPool := x509.NewCertPool()
		caPEM, err := os.ReadFile(b.options.ClientCAFile)
		if err != nil {
			return fmt.Errorf("failed to read server client CA file: %v", err)
		}
		if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
			return fmt.Errorf("failed to append server client CA to cert pool")
		}
		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	}

	grpcServerOptions = append(grpcServerOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))

	// append the stats handler for metrics
	grpcServerOptions = append(grpcServerOptions, grpc.StatsHandler(metrics.NewGRPCMetricsHandler()))

	// init prometheus middleware for grpc server
	promMiddleware := grpcprom.NewServerMetrics(
		// enable grpc handling time histogram to measure latency distributions of RPCs
		grpcprom.WithServerHandlingTimeHistogram(
			grpcprom.WithHistogramBuckets(k8smetrics.ExponentialBuckets(10e-7, 10, 10)),
		),
	)

	grpcServerOptions = append(grpcServerOptions,
		grpc.ChainUnaryInterceptor(
			metrics.NewGRPCMetricsUnaryInterceptor(promMiddleware),
			newAuthnUnaryInterceptor(b.authenticators...),
			newAuthzUnaryInterceptor(b.unaryAuthorizers...),
		),
		grpc.ChainStreamInterceptor(
			metrics.NewGRPCMetricsStreamInterceptor(promMiddleware),
			newAuthnStreamInterceptor(b.authenticators...),
			newAuthzStreamInterceptor(b.streamAuthorizers),
		))

	grpcServer := grpc.NewServer(grpcServerOptions...)
	// register all the general grpc server metrics
	metrics.RegisterGRPCMetrics(promMiddleware, b.extraMetrics...)
	// initialize grpc server metrics with appropriate value.
	promMiddleware.InitializeMetrics(grpcServer)

	for _, r := range b.registerFuncs {
		r(grpcServer)
	}

	// Start gRPC server
	logger := klog.FromContext(ctx)
	addr := ":" + b.options.ServerBindPort
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	logger.Info("Starting gRPC server", "addr", lis.Addr().String())

	serveErrCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			serveErrCh <- fmt.Errorf("failed to serve gRPC server: %w", err)
		} else {
			serveErrCh <- nil
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("Shutting down gRPC server")
		grpcServer.GracefulStop()
		return nil
	case err := <-serveErrCh:
		// If Serve returns early with error, surface it
		return err
	}
}

func newAuthnUnaryInterceptor(authenticators ...authn.Authenticator) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		var err error
		for _, authenticator := range authenticators {
			ctx, err = authenticator.Authenticate(ctx)
			if err == nil {
				return handler(ctx, req)
			}
		}

		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

func newAuthzUnaryInterceptor(authorizers ...authz.UnaryAuthorizer) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		var errs []error
		for _, authorizer := range authorizers {
			decision, err := authorizer.AuthorizeRequest(ctx, req)
			switch decision {
			case authz.DecisionAllow:
				return handler(ctx, req)
			case authz.DecisionDeny:
				return nil, fmt.Errorf("access denied: %v", err)
			case authz.DecisionNoOpinion:
				if err != nil {
					errs = append(errs, err)
				}
				// Continue to next authorizer
			}
		}

		if len(errs) > 0 {
			return nil, errors.NewAggregate(errs)
		}

		return nil, fmt.Errorf("no authorizer found for %s", info.FullMethod)
	}
}

// wrappedAuthStream wraps a grpc.ServerStream associated with an incoming RPC, and
// a custom context containing the user and groups derived from the client certificate
// specified in the incoming RPC metadata
type wrappedAuthStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the context associated with the stream
func (w *wrappedAuthStream) Context() context.Context {
	return w.ctx
}

// newWrappedAuthStream creates a new wrappedAuthStream
func newWrappedAuthStream(ctx context.Context, s grpc.ServerStream) grpc.ServerStream {
	return &wrappedAuthStream{s, ctx}
}

// newAuthnStreamInterceptor creates a stream interceptor that retrieves the user and groups
// based on the specified authentication type. It supports retrieving from either the access
// token or the client certificate depending on the provided authNType.
// The interceptor then adds the retrieved identity information (user and groups) to the
// context and invokes the provided handler.
func newAuthnStreamInterceptor(authenticators ...authn.Authenticator) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		var err error
		ctx := ss.Context()
		for _, authenticator := range authenticators {
			ctx, err = authenticator.Authenticate(ctx)
			if err == nil {
				return handler(srv, newWrappedAuthStream(ctx, ss))
			}
		}

		if err != nil {
			return err
		}

		return handler(srv, newWrappedAuthStream(ctx, ss))
	}
}

// newAuthzStreamInterceptor is a stream interceptor that authorizes the stream request.
func newAuthzStreamInterceptor(authorizers []authz.StreamAuthorizer) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		var errs []error

		for _, authorizer := range authorizers {
			decision, authorizedStream, err := authorizer.AuthorizeStream(ss.Context(), ss, info)
			switch decision {
			case authz.DecisionAllow:
				return handler(srv, authorizedStream)
			case authz.DecisionDeny:
				return fmt.Errorf("access denied: %v", err)
			case authz.DecisionNoOpinion:
				if err != nil {
					errs = append(errs, err)
				}
				// Continue to next authorizer
			}
		}

		if len(errs) > 0 {
			return errors.NewAggregate(errs)
		}

		return fmt.Errorf("no authorizer found for %s", info.FullMethod)
	}
}
