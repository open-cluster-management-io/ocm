package operator

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/nucleus/pkg/operators"
	"github.com/open-cluster-management/nucleus/pkg/version"
)

// NewOperatorCmd generatee a command to start workload agent
func NewOperatorCmd() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("nucleus-operator", version.Get(), operators.RunNucleusOperator).
		NewCommand()
	cmd.Use = "operator"
	cmd.Short = "Start the nucleus operator"

	return cmd
}
