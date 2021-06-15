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
	cmd := controllercmd.
		NewControllerCommandConfig("work-agent", version.Get(), o.RunWorkloadAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the Cluster Registration Agent"

	o.AddFlags(cmd)
	return cmd
}
