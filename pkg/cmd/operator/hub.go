package operator

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/nucleus/pkg/operators"
	"github.com/open-cluster-management/nucleus/pkg/version"
)

// NewHubOperatorCmd generatee a command to start hub operator
func NewHubOperatorCmd() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("nucleus-hub", version.Get(), operators.RunNucleusHubOperator).
		NewCommand()
	cmd.Use = "hub"
	cmd.Short = "Start the nucleus hub operator"

	return cmd
}
