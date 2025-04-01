package options

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"k8s.io/klog/v2"

	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
	grpcserver "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authn"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authz"
)

// PreStartHook is an interface to start hook before grpc server is started.
type PreStartHook interface {
	// Start should be a non-blocking call
	Run(ctx context.Context)
}

type Server struct {
	options        *GRPCServerOptions
	authenticators []authn.Authenticator
	authorizers    []authz.Authorizer
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

func (s *Server) WithAuthorizer(authorizer authz.Authorizer) *Server {
	s.authorizers = append(s.authorizers, authorizer)
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
		grpc.ChainUnaryInterceptor(
			newAuthnUnaryInterceptor(s.authenticators...),
			newAuthzUnaryInterceptor(s.authorizers...)),
		grpc.ChainStreamInterceptor(
			newAuthnStreamInterceptor(s.authenticators...),
			newAuthzStreamInterceptor(s.authorizers...)))

	grpcServer := grpc.NewServer(grpcServerOptions...)
	grpcEventServer := grpcserver.NewGRPCBroker(grpcServer)

	for t, service := range s.services {
		grpcEventServer.RegisterService(t, service)
	}

	// start hook
	for _, hook := range s.hooks {
		hook.Run(ctx)
	}

	go grpcEventServer.Start(ctx, ":"+s.options.ServerBindPort)
	<-ctx.Done()
	return nil
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

func newAuthzUnaryInterceptor(authorizers ...authz.Authorizer) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		for _, authorizer := range authorizers {
			pReq, ok := req.(*pbv1.PublishRequest)
			if !ok {
				return nil, fmt.Errorf("unsupported request type %T", req)
			}

			eventsType, err := types.ParseCloudEventsType(pReq.Event.Type)
			if err != nil {
				return nil, err
			}

			// the event of grpc publish request is the original cloudevent data, we need a `ce-` prefix
			// to get the event attribute
			clusterAttr, ok := pReq.Event.Attributes[fmt.Sprintf("ce-%s", types.ExtensionClusterName)]
			if !ok {
				return nil, fmt.Errorf("missing ce-clustername in event attributes, %v", pReq.Event.Attributes)
			}

			if err := authorizer.Authorize(ctx, clusterAttr.GetCeString(), *eventsType); err != nil {
				return nil, err
			}
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

// wrappedAuthorizedStream caches the subscription request that is already read.
type wrappedAuthorizedStream struct {
	sync.Mutex

	grpc.ServerStream
	authorizedReq *pbv1.SubscriptionRequest
}

// RecvMsg set the msg from the cache.
func (c *wrappedAuthorizedStream) RecvMsg(m any) error {
	c.Lock()
	defer c.Unlock()

	msg, ok := m.(*pbv1.SubscriptionRequest)
	if !ok {
		return fmt.Errorf("unsupported request type %T", m)
	}

	msg.ClusterName = c.authorizedReq.ClusterName
	msg.Source = c.authorizedReq.Source
	msg.DataType = c.authorizedReq.DataType
	return nil
}

// newAuthzStreamInterceptor is a stream interceptor that authorizes the subscription request.
func newAuthzStreamInterceptor(authorizers ...authz.Authorizer) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if info.IsClientStream {
			return handler(srv, ss)
		}

		var req pbv1.SubscriptionRequest
		if err := ss.RecvMsg(&req); err != nil {
			return err
		}

		eventDataType, err := types.ParseCloudEventsDataType(req.DataType)
		if err != nil {
			return err
		}

		eventsType := types.CloudEventsType{
			CloudEventsDataType: *eventDataType,
			SubResource:         types.SubResourceSpec,
			Action:              types.WatchRequestAction,
		}
		for _, authorizer := range authorizers {
			if err := authorizer.Authorize(ss.Context(), req.ClusterName, eventsType); err != nil {
				return err
			}
		}

		if err := handler(srv, &wrappedAuthorizedStream{ServerStream: ss, authorizedReq: &req}); err != nil {
			klog.Error(err)
			return err
		}

		return nil
	}
}
