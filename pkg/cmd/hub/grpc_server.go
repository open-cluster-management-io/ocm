package hub

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"k8s.io/utils/clock"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	grpcopts "open-cluster-management.io/ocm/pkg/server/grpc"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewGRPCServerCommand() *cobra.Command {
	opts := commonoptions.NewOptions()
	grpcServerOpts := grpcopts.NewGRPCServerOptions()

	// Disable leader election to allow multiple gRPC server instances to run concurrently.
	cmdConfig := controllercmd.NewControllerCommandConfig("grpc-server", version.Get(),
		opts.StartWithQPS(grpcStartFunc(opts, grpcServerOpts)), clock.RealClock{})
	cmdConfig.DisableLeaderElection = true

	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "grpc"
	cmd.Short = "Start the gRPC Server"

	flags := cmd.Flags()
	opts.AddFlags(flags)
	grpcServerOpts.AddFlags(flags)
	opts.ApplyTLSToCommand(cmd)

	return cmd
}

// grpcStartFunc bridges TLS flags from common options to gRPC server options
// before starting the gRPC server. Extracted for testability.
func grpcStartFunc(opts *commonoptions.Options, grpcServerOpts *grpcopts.GRPCServerOptions) controllercmd.StartFunc {
	return func(ctx context.Context, cc *controllercmd.ControllerContext) error {
		grpcServerOpts.TLSMinVersionOverride = opts.TLSMinVersion
		grpcServerOpts.TLSCipherSuitesOverride = opts.TLSCipherSuites
		return grpcServerOpts.Run(ctx, cc)
	}
}
