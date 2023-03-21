package main

import (
	goflag "flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"

	"open-cluster-management.io/work/pkg/cmd/hub"
	"open-cluster-management.io/work/pkg/cmd/spoke"
	"open-cluster-management.io/work/pkg/cmd/webhook"
	"open-cluster-management.io/work/pkg/version"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.AddFlags(pflag.CommandLine)
	logs.InitLogs()
	defer logs.FlushLogs()

	command := newWorkCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newWorkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Spoke Cluster Work",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(spoke.NewWorkloadAgent())
	cmd.AddCommand(webhook.NewWebhook())
	cmd.AddCommand(hub.NewHubManager())
	return cmd
}
