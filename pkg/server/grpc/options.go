package grpc

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1alpha1"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	cloudeventsgrpc "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc"
	grpcauthz "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authz/kube"
	cemetrics "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/metrics"
	sdkgrpc "open-cluster-management.io/sdk-go/pkg/server/grpc"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"

	"open-cluster-management.io/ocm/pkg/server/services/addon"
	"open-cluster-management.io/ocm/pkg/server/services/cluster"
	"open-cluster-management.io/ocm/pkg/server/services/csr"
	"open-cluster-management.io/ocm/pkg/server/services/event"
	"open-cluster-management.io/ocm/pkg/server/services/lease"
	"open-cluster-management.io/ocm/pkg/server/services/work"
)

type GRPCServerOptions struct {
	GRPCServerConfig string
}

func NewGRPCServerOptions() *GRPCServerOptions {
	return &GRPCServerOptions{}
}

func (o *GRPCServerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.GRPCServerConfig, "server-config", o.GRPCServerConfig, "Location of the server configuration file.")
}

func (o *GRPCServerOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	serverOptions, err := sdkgrpc.LoadGRPCServerOptions(o.GRPCServerConfig)
	if err != nil {
		return err
	}

	clients, err := NewClients(controllerContext)
	if err != nil {
		return err
	}

	// initlize grpc broker and register services
	grpcEventServer := cloudeventsgrpc.NewGRPCBroker()
	grpcEventServer.RegisterService(ctx, clusterce.ManagedClusterEventDataType,
		cluster.NewClusterService(clients.ClusterClient, clients.ClusterInformers.Cluster().V1().ManagedClusters()))
	grpcEventServer.RegisterService(ctx, csrce.CSREventDataType,
		csr.NewCSRService(clients.KubeClient, clients.KubeInformers.Certificates().V1().CertificateSigningRequests()))
	grpcEventServer.RegisterService(ctx, addonce.ManagedClusterAddOnEventDataType,
		addon.NewAddonService(clients.AddOnClient, clients.AddOnInformers.Addon().V1alpha1().ManagedClusterAddOns()))
	grpcEventServer.RegisterService(ctx, eventce.EventEventDataType,
		event.NewEventService(clients.KubeClient))
	grpcEventServer.RegisterService(ctx, leasece.LeaseEventDataType,
		lease.NewLeaseService(clients.KubeClient, clients.KubeInformers.Coordination().V1().Leases()))
	grpcEventServer.RegisterService(ctx, payload.ManifestBundleEventDataType,
		work.NewWorkService(clients.WorkClient, clients.WorkInformers.Work().V1().ManifestWorks()))

	// start clients
	go clients.Run(ctx)

	// initlize and run grpc server
	authorizer := grpcauthz.NewSARAuthorizer(clients.KubeClient)
	return sdkgrpc.NewGRPCServer(serverOptions).
		WithAuthenticator(grpcauthn.NewTokenAuthenticator(clients.KubeClient)).
		WithAuthenticator(grpcauthn.NewMtlsAuthenticator()).
		WithUnaryAuthorizer(authorizer).
		WithStreamAuthorizer(authorizer).
		WithRegisterFunc(func(s *grpc.Server) {
			pbv1.RegisterCloudEventServiceServer(s, grpcEventServer)
		}).
		WithExtraMetrics(cemetrics.CloudEventsGRPCMetrics()...).
		Run(ctx)
}
