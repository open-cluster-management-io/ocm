package grpc

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	grpcserver "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/server/services"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewGRPCServer() *cobra.Command {
	opts := commonoptions.NewOptions()
	grpcServerOpts := NewGRPCServerOptions()
	cmdConfig := opts.
		NewControllerCommandConfig("grpc-server", version.Get(), grpcServerOpts.Run)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "grpc-server"
	cmd.Short = "Start the gRPC Server"

	flags := cmd.Flags()
	opts.AddFlags(flags)
	grpcServerOpts.AddFlags(flags)

	return cmd
}

type grpcServerOptions struct {
}

func NewGRPCServerOptions() *grpcServerOptions {
	return &grpcServerOptions{}
}

func (o *grpcServerOptions) AddFlags(flags *pflag.FlagSet) {
}

func (o *grpcServerOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	grpcServerOptions := []grpc.ServerOption{}
	grpcServer := grpc.NewServer(grpcServerOptions...)
	grpcEventServer := grpcserver.NewGRPCBroker(grpcServer, ":8888")
	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	addonClient, err := addonv1alpha1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 30*time.Minute)
	kubeInformers := kubeinformers.NewSharedInformerFactory(kubeClient, 30*time.Minute)
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
	go clusterInformers.Start(ctx.Done())
	go kubeInformers.Start(ctx.Done())
	go addonInformers.Start(ctx.Done())
	go grpcEventServer.Start(ctx)

	<-ctx.Done()
	return nil
}
