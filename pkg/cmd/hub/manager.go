package hub

import (
	"github.com/spf13/cobra"

	"open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/addon-framework/pkg/manager"
	"open-cluster-management.io/addon-framework/pkg/version"
)

// NewHubManager generates a command to start hub manager
func NewHubManager() *cobra.Command {
	cmdConfig := factory.
		NewControllerCommandConfig("manager", version.Get(), manager.RunManager)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Start the Addon Manager"

	return cmd
}
