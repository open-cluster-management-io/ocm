package grpc

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"

	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authn"
	grpcauthz "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authz/kube"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/options"

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
	serverOptions, err := grpcoptions.LoadGRPCServerOptions(o.GRPCServerConfig)
	if err != nil {
		return err
	}

	clients, err := NewClients(controllerContext)
	if err != nil {
		return err
	}

	return grpcoptions.NewServer(serverOptions).WithPreStartHooks(clients).WithAuthenticator(
		grpcauthn.NewTokenAuthenticator(clients.KubeClient),
	).WithAuthenticator(
		grpcauthn.NewMtlsAuthenticator(),
	).WithAuthorizer(
		grpcauthz.NewSARAuthorizer(clients.KubeClient),
	).WithService(
		clusterce.ManagedClusterEventDataType,
		cluster.NewClusterService(clients.ClusterClient, clients.ClusterInformers.Cluster().V1().ManagedClusters()),
	).WithService(
		csrce.CSREventDataType,
		csr.NewCSRService(clients.KubeClient, clients.KubeInformers.Certificates().V1().CertificateSigningRequests()),
	).WithService(
		addonce.ManagedClusterAddOnEventDataType,
		addon.NewAddonService(clients.AddOnClient, clients.AddOnInformers.Addon().V1alpha1().ManagedClusterAddOns()),
	).WithService(
		eventce.EventEventDataType,
		event.NewEventService(clients.KubeClient),
	).WithService(
		leasece.LeaseEventDataType,
		lease.NewLeaseService(clients.KubeClient, clients.KubeInformers.Coordination().V1().Leases()),
	).WithService(
		payload.ManifestBundleEventDataType,
		work.NewWorkService(clients.WorkClient, clients.WorkInformers.Work().V1().ManifestWorks()),
	).Run(ctx)
}
