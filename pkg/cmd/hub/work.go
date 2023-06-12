package hub

import (
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"

	"open-cluster-management.io/ocm/pkg/version"
	"open-cluster-management.io/ocm/pkg/work/hub"
)

// NewHubManager generates a command to start hub manager
func NewWorkController() *cobra.Command {
	cmdConfig := controllercmd.
		NewControllerCommandConfig("work-manager", version.Get(), hub.RunWorkHubManager)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the Work Hub Manager"

	return cmd
}
