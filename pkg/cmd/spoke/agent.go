package spoke

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"open-cluster-management.io/work/pkg/spoke"
	"open-cluster-management.io/work/pkg/version"
)

// NewWorkloadAgent generates a command to start workload agent
func NewWorkloadAgent() *cobra.Command {
	o := spoke.NewWorkloadAgentOptions()
	cmdConfig := controllercmd.
		NewControllerCommandConfig("work-agent", version.Get(), o.RunWorkloadAgent)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the Cluster Registration Agent"

	o.AddFlags(cmd)

	// add disable leader election flag
	flags := cmd.Flags()
	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the agent.")

	return cmd
}
