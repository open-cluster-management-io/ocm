package main

import (
	goflag "flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"

	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/cmd/hub"
	"open-cluster-management.io/ocm/pkg/cmd/spoke"
	"open-cluster-management.io/ocm/pkg/cmd/webhook"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/server/grpc"
	"open-cluster-management.io/ocm/pkg/version"
)

// The registration binary contains both the hub-side controllers for the
// registration API and the spoke agent.

func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.AddFlags(pflag.CommandLine)
	logs.InitLogs()
	defer logs.FlushLogs()

	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	features.HubMutableFeatureGate.AddFlag(pflag.CommandLine)

	command := newRegistrationCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newRegistrationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registration",
		Short: "Spoke Cluster Registration",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
			os.Exit(1)
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(hub.NewRegistrationController())
	cmd.AddCommand(spoke.NewRegistrationAgent())
	cmd.AddCommand(webhook.NewRegistrationWebhook())
	cmd.AddCommand(grpc.NewGRPCServer())

	return cmd
}
