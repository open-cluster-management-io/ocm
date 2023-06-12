package spoke

import (
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"

	"open-cluster-management.io/ocm/pkg/version"
	"open-cluster-management.io/ocm/pkg/work/spoke"
)

// NewWorkAgent generates a command to start work agent
func NewWorkAgent() *cobra.Command {
	o := spoke.NewWorkloadAgentOptions()
	cmdConfig := controllercmd.
		NewControllerCommandConfig("work-agent", version.Get(), o.RunWorkloadAgent)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the Work Agent"

	o.AddFlags(cmd)

	// add disable leader election flag
	flags := cmd.Flags()
	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the agent.")

	return cmd
}
