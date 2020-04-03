package spoke

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/registration/pkg/spoke"
	"github.com/open-cluster-management/registration/pkg/version"
)

func NewAgent() *cobra.Command {
	agent := spoke.NewAgent()
	cmd := controllercmd.
		NewControllerCommandConfig("agent", version.Get(), agent.RunAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the Cluster Registration Agent"

	agent.AddFlags(cmd.Flags())
	return cmd
}
