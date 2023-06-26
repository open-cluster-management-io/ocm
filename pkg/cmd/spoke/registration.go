package spoke

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"

	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewRegistrationAgent() *cobra.Command {
	agentOptions := spoke.NewSpokeAgentOptions()
	commonOptions := commonoptions.NewAgentOptions()
	cfg := spoke.NewSpokeAgentConfig(commonOptions, agentOptions)
	cmdConfig := controllercmd.
		NewControllerCommandConfig("registration-agent", version.Get(), cfg.RunSpokeAgent)

	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "agent"
	cmd.Short = "Start the Cluster Registration Agent"

	flags := cmd.Flags()
	commonOptions.AddFlags(flags)
	agentOptions.AddFlags(flags)

	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the agent.")
	return cmd
}
