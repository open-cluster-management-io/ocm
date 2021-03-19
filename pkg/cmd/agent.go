package cmd

import (
	"github.com/open-cluster-management/addon-framework/pkg/spoke"
	"github.com/open-cluster-management/addon-framework/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
)

func NewAgent() *cobra.Command {
	agentOptions := spoke.NewSpokeAgentOptions()
	cmd := controllercmd.
		NewControllerCommandConfig("agent", version.Get(), agentOptions.RunSpokeAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the Addon Manager Agent"
	agentOptions.AddFlags(cmd.Flags())
	return cmd
}
