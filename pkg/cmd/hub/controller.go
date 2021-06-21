package hub

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	controllers "open-cluster-management.io/placement/pkg/controllers"
	"open-cluster-management.io/placement/pkg/version"
)

func NewController() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("placement", version.Get(), controllers.RunControllerManager).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the Placement Scheduling Controller"

	return cmd
}
