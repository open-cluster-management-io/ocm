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
		NewControllerCommandConfig("klusterlet", version.Get(), operators.RunKlusterletOperator).
		NewCommand()
	cmd.Use = "spoke"
	cmd.Short = "Start the klusterlet operator"

	return cmd
}
