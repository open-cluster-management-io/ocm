package hub

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"open-cluster-management.io/work/pkg/hub"
	"open-cluster-management.io/work/pkg/version"
)

// NewHubManager generates a command to start hub manager
func NewHubManager() *cobra.Command {
	cmdConfig := controllercmd.
		NewControllerCommandConfig("work-manager", version.Get(), hub.RunWorkHubManager)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the Work Hub Manager"

	return cmd
}
