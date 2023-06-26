package hub

import (
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"

	"open-cluster-management.io/ocm/pkg/addon"
	"open-cluster-management.io/ocm/pkg/version"
)

// NewAddonManager generates a command to start addon manager
func NewAddonManager() *cobra.Command {
	cmdConfig := controllercmd.
		NewControllerCommandConfig("manager", version.Get(), addon.RunManager)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the Addon Manager"

	return cmd
}
