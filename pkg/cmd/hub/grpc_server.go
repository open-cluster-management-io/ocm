package hub

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"k8s.io/utils/clock"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/server/grpc"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewGRPCServerCommand() *cobra.Command {
	opts := commonoptions.NewOptions()
	grpcServerOpts := grpc.NewGRPCServerOptions()

	// Disable leader election to allow multiple gRPC server instances to run concurrently.
	cmdConfig := controllercmd.NewControllerCommandConfig("grpc-server", version.Get(), opts.StartWithQPS(grpcServerOpts.Run), clock.RealClock{})
	cmdConfig.DisableLeaderElection = true

	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "grpc"
	cmd.Short = "Start the gRPC Server"

	flags := cmd.Flags()
	opts.AddFlags(flags)
	grpcServerOpts.AddFlags(flags)

	return cmd
}
