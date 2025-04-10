package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math"
	"net"
	"os"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	grpcserver "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/server/grpc/authn"
	"open-cluster-management.io/ocm/pkg/server/services"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewGRPCServer() *cobra.Command {
	opts := commonoptions.NewOptions()
	grpcServerOpts := NewGRPCServerOptions()
	cmdConfig := opts.
		NewControllerCommandConfig("grpc-server", version.Get(), grpcServerOpts.Run, clock.RealClock{})
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "grpc-server"
	cmd.Short = "Start the gRPC Server"

	flags := cmd.Flags()
	opts.AddFlags(flags)
	grpcServerOpts.AddFlags(flags)

	return cmd
}

type GRPCServerOptions struct {
	TLSCertFile             string
	TLSKeyFile              string
	ClientCAFile            string
	ServerBindPort          string
	MaxConcurrentStreams    uint32
	MaxReceiveMessageSize   int
	MaxSendMessageSize      int
	ConnectionTimeout       time.Duration
	WriteBufferSize         int
	ReadBufferSize          int
	MaxConnectionAge        time.Duration
	ClientMinPingInterval   time.Duration
	ServerPingInterval      time.Duration
	ServerPingTimeout       time.Duration
	PermitPingWithoutStream bool
}

func NewGRPCServerOptions() *GRPCServerOptions {
	return &GRPCServerOptions{}
}

func (o *GRPCServerOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.ServerBindPort, "grpc-server-bindport", "8090", "gPRC server bind port")
	flags.Uint32Var(&o.MaxConcurrentStreams, "grpc-max-concurrent-streams", math.MaxUint32, "gPRC max concurrent streams")
	flags.IntVar(&o.MaxReceiveMessageSize, "grpc-max-receive-message-size", 1024*1024*4, "gPRC max receive message size")
	flags.IntVar(&o.MaxSendMessageSize, "grpc-max-send-message-size", math.MaxInt32, "gPRC max send message size")
	flags.DurationVar(&o.ConnectionTimeout, "grpc-connection-timeout", 120*time.Second, "gPRC connection timeout")
	flags.DurationVar(&o.MaxConnectionAge, "grpc-max-connection-age", time.Duration(math.MaxInt64), "A duration for the maximum amount of time connection may exist before closing")
	flags.DurationVar(&o.ClientMinPingInterval, "grpc-client-min-ping-interval", 5*time.Second, "Server will terminate the connection if the client pings more than once within this duration")
	flags.DurationVar(&o.ServerPingInterval, "grpc-server-ping-interval", 30*time.Second, "Duration after which the server pings the client if no activity is detected")
	flags.DurationVar(&o.ServerPingTimeout, "grpc-server-ping-timeout", 10*time.Second, "Duration the client waits for a response after sending a keepalive ping")
	flags.BoolVar(&o.PermitPingWithoutStream, "permit-ping-without-stream", false, "Allow keepalive pings even when there are no active streams")
	flags.IntVar(&o.WriteBufferSize, "grpc-write-buffer-size", 32*1024, "gPRC write buffer size")
	flags.IntVar(&o.ReadBufferSize, "grpc-read-buffer-size", 32*1024, "gPRC read buffer size")
	flags.StringVar(&o.TLSCertFile, "grpc-tls-cert-file", "", "The path to the tls.crt file")
	flags.StringVar(&o.TLSKeyFile, "grpc-tls-key-file", "", "The path to the tls.key file")
	flags.StringVar(&o.ClientCAFile, "grpc-client-ca-file", "", "The path to the client ca file, must specify if using mtls authentication type")
}

func (o *GRPCServerOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	var grpcServerOptions []grpc.ServerOption
	grpcServerOptions = append(grpcServerOptions, grpc.MaxRecvMsgSize(o.MaxReceiveMessageSize))
	grpcServerOptions = append(grpcServerOptions, grpc.MaxSendMsgSize(o.MaxSendMessageSize))
	grpcServerOptions = append(grpcServerOptions, grpc.MaxConcurrentStreams(o.MaxConcurrentStreams))
	grpcServerOptions = append(grpcServerOptions, grpc.ConnectionTimeout(o.ConnectionTimeout))
	grpcServerOptions = append(grpcServerOptions, grpc.WriteBufferSize(o.WriteBufferSize))
	grpcServerOptions = append(grpcServerOptions, grpc.ReadBufferSize(o.ReadBufferSize))
	grpcServerOptions = append(grpcServerOptions, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             o.ClientMinPingInterval,
		PermitWithoutStream: o.PermitPingWithoutStream,
	}))
	grpcServerOptions = append(grpcServerOptions, grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionAge: o.MaxConnectionAge,
		Time:             o.ServerPingInterval,
		Timeout:          o.ServerPingTimeout,
	}))

	logger := klog.FromContext(ctx)
	if o.TLSCertFile == "" && o.TLSKeyFile == "" {
		cert, key, err := certutil.GenerateSelfSignedCertKey("localhost", []net.IP{net.ParseIP("127.0.0.1")}, nil)
		if err != nil {
			return err
		}
		path, err := os.MkdirTemp("", "certs")
		if err != nil {
			return err
		}
		err = os.WriteFile(path+"/tls.crt", cert, 0600)
		if err != nil {
			return err
		}
		o.TLSCertFile = path + "/tls.crt"
		err = os.WriteFile(path+"/tls.key", key, 0600)
		if err != nil {
			return err
		}
		o.TLSKeyFile = path + "/tls.key"
		logger.Info("Using self-signed TLS certificate", "cert", o.TLSCertFile, "key", o.TLSKeyFile)
	}

	// Serve with TLS
	serverCerts, err := tls.LoadX509KeyPair(o.TLSCertFile, o.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("failed to load broker certificates: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCerts},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
	}

	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	authenticators := []authn.Authenticator{
		authn.NewTokenAuthenticator(kubeClient),
	}

	if o.ClientCAFile != "" {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return fmt.Errorf("failed to load system cert pool: %v", err)
		}
		caPEM, err := os.ReadFile(o.ClientCAFile)
		if err != nil {
			return fmt.Errorf("failed to read broker client CA file: %v", err)
		}
		if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
			return fmt.Errorf("failed to append broker client CA to cert pool")
		}
		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
		authenticators = append(authenticators, authn.NewMtlsAuthenticator())
	}
	grpcServerOptions = append(grpcServerOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))

	grpcServerOptions = append(grpcServerOptions,
		grpc.ChainUnaryInterceptor(newAuthUnaryInterceptor(authenticators...)),
		grpc.ChainStreamInterceptor(newAuthStreamInterceptor(authenticators...)))

	grpcServer := grpc.NewServer(grpcServerOptions...)
	grpcEventServer := grpcserver.NewGRPCBroker(grpcServer, ":8888")
	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	addonClient, err := addonv1alpha1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 30*time.Minute)
	kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 30*time.Minute,
		kubeinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      clusterv1.ClusterNameLabelKey,
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}))

	grpcEventServer.RegisterService(
		cluster.ManagedClusterEventDataType,
		services.NewClusterService(clusterClient, clusterInformers.Cluster().V1().ManagedClusters()))
	grpcEventServer.RegisterService(
		csr.CSREventDataType,
		services.NewCSRService(kubeClient, kubeInformers.Certificates().V1().CertificateSigningRequests()))
	grpcEventServer.RegisterService(
		addonce.ManagedClusterAddOnEventDataType,
		services.NewAddonService(addonClient, addonInformers.Addon().V1alpha1().ManagedClusterAddOns()),
	)
	grpcEventServer.RegisterService(
		eventce.EventEventDataType,
		services.NewEventService(kubeClient),
	)
	grpcEventServer.RegisterService(
		leasece.LeaseEventDataType,
		services.NewLeaseService(kubeClient, kubeInformers.Coordination().V1().Leases()),
	)

	go clusterInformers.Start(ctx.Done())
	go kubeInformers.Start(ctx.Done())
	go addonInformers.Start(ctx.Done())
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
