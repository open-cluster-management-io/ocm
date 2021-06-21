package operator

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"open-cluster-management.io/registration-operator/pkg/operators"
	"open-cluster-management.io/registration-operator/pkg/version"
)

// NewHubOperatorCmd generatee a command to start hub operator
func NewHubOperatorCmd() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("clustermanager", version.Get(), operators.RunClusterManagerOperator).
		NewCommand()
	cmd.Use = "hub"
	cmd.Short = "Start the cluster manager operator"

	return cmd
}
