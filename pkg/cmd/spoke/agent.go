package spoke

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/pkg/version"
)

func NewAgent() *cobra.Command {
	agentOptions := spoke.NewSpokeAgentOptions()
	cmdConfig := controllercmd.
		NewControllerCommandConfig("registration-agent", version.Get(), agentOptions.RunSpokeAgent)

	cmd := cmdConfig.NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the Cluster Registration Agent"

	flags := cmd.Flags()
	agentOptions.AddFlags(flags)

	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the agent.")
	return cmd
}
