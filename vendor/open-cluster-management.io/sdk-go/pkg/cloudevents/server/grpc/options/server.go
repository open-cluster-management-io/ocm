package options

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
	grpcserver "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authn"
	"os"
)

// PreStartHook is an interface to start hook before grpc server is started.
type PreStartHook interface {
	// Start should be a non-blocking call
	Run(ctx context.Context)
}

type Server struct {
	options        *GRPCServerOptions
	authenticators []authn.Authenticator
	services       map[types.CloudEventsDataType]server.Service
	hooks          []PreStartHook
}

func NewServer(opt *GRPCServerOptions) *Server {
	return &Server{options: opt, services: make(map[types.CloudEventsDataType]server.Service)}
}

func (s *Server) WithAuthenticator(authenticator authn.Authenticator) *Server {
	s.authenticators = append(s.authenticators, authenticator)
	return s
}

func (s *Server) WithService(t types.CloudEventsDataType, service server.Service) *Server {
	s.services[t] = service
	return s
}

func (s *Server) WithPreStartHooks(hooks ...PreStartHook) *Server {
	s.hooks = append(s.hooks, hooks...)
	return s
}

func (s *Server) Run(ctx context.Context) error {
	var grpcServerOptions []grpc.ServerOption
	grpcServerOptions = append(grpcServerOptions, grpc.MaxRecvMsgSize(s.options.MaxReceiveMessageSize))
	grpcServerOptions = append(grpcServerOptions, grpc.MaxSendMsgSize(s.options.MaxSendMessageSize))
	grpcServerOptions = append(grpcServerOptions, grpc.MaxConcurrentStreams(s.options.MaxConcurrentStreams))
	grpcServerOptions = append(grpcServerOptions, grpc.ConnectionTimeout(s.options.ConnectionTimeout))
	grpcServerOptions = append(grpcServerOptions, grpc.WriteBufferSize(s.options.WriteBufferSize))
	grpcServerOptions = append(grpcServerOptions, grpc.ReadBufferSize(s.options.ReadBufferSize))
	grpcServerOptions = append(grpcServerOptions, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             s.options.ClientMinPingInterval,
		PermitWithoutStream: s.options.PermitPingWithoutStream,
	}))
	grpcServerOptions = append(grpcServerOptions, grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionAge: s.options.MaxConnectionAge,
		Time:             s.options.ServerPingInterval,
		Timeout:          s.options.ServerPingTimeout,
	}))

	// Serve with TLS
	serverCerts, err := tls.LoadX509KeyPair(s.options.TLSCertFile, s.options.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("failed to load broker certificates: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCerts},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
	}

	if s.options.ClientCAFile != "" {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return fmt.Errorf("failed to load system cert pool: %v", err)
		}
		caPEM, err := os.ReadFile(s.options.ClientCAFile)
		if err != nil {
			return fmt.Errorf("failed to read broker client CA file: %v", err)
		}
		if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
			return fmt.Errorf("failed to append broker client CA to cert pool")
		}
		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	}

	grpcServerOptions = append(grpcServerOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))

	grpcServerOptions = append(grpcServerOptions,
		grpc.ChainUnaryInterceptor(newAuthUnaryInterceptor(s.authenticators...)),
		grpc.ChainStreamInterceptor(newAuthStreamInterceptor(s.authenticators...)))

	grpcServer := grpc.NewServer(grpcServerOptions...)
	grpcEventServer := grpcserver.NewGRPCBroker(grpcServer, ":"+s.options.ServerBindPort)

	for t, service := range s.services {
		grpcEventServer.RegisterService(t, service)
	}

	// start hook
	for _, hook := range s.hooks {
		hook.Run(ctx)
	}

	go grpcEventServer.Start(ctx)
	<-ctx.Done()
	return nil
}

func newAuthUnaryInterceptor(authenticators ...authn.Authenticator) grpc.UnaryServerInterceptor {
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

// newAuthStreamInterceptor creates a stream interceptor that retrieves the user and groups
// based on the specified authentication type. It supports retrieving from either the access
// token or the client certificate depending on the provided authNType.
// The interceptor then adds the retrieved identity information (user and groups) to the
// context and invokes the provided handler.
func newAuthStreamInterceptor(authenticators ...authn.Authenticator) grpc.StreamServerInterceptor {
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
