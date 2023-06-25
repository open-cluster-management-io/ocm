package hub

import (
	"github.com/spf13/cobra"

	"open-cluster-management.io/addon-framework/pkg/cmd/factory"

	"open-cluster-management.io/ocm/pkg/addon"
	"open-cluster-management.io/ocm/pkg/version"
)

// NewAddonManager generates a command to start addon manager
func NewAddonManager() *cobra.Command {
	cmdConfig := factory.
		NewControllerCommandConfig("manager", version.Get(), addon.RunManager)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the Addon Manager"

	return cmd
}
