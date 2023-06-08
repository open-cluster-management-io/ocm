package hub

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewPlacementController() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("placement", version.Get(), controllers.RunControllerManager).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the Placement Scheduling Controller"

	return cmd
}
