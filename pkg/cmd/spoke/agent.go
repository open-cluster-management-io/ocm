package spoke

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/work/pkg/spoke"
	"github.com/open-cluster-management/work/pkg/version"
)

// NewWorkloadAgent generatee a command to start workload agent
func NewWorkloadAgent() *cobra.Command {
	o := spoke.NewWorkloadAgentOptions()
	cmd := controllercmd.
		NewControllerCommandConfig("agent", version.Get(), o.RunWorkloadAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the Cluster Registration Agent"

	o.AddFlags(cmd)
	return cmd
}
