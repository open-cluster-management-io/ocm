package operator

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/nucleus/pkg/operators"
	"github.com/open-cluster-management/nucleus/pkg/version"
)

// NewSpokeOperatorCmd generatee a command to start spoke operator
func NewSpokeOperatorCmd() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("nucleus-spoke", version.Get(), operators.RunNucleusSpokeOperator).
		NewCommand()
	cmd.Use = "spoke"
	cmd.Short = "Start the nucleus hub operator"

	return cmd
}
