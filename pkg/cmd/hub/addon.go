package hub

import (
	"context"

	"github.com/spf13/cobra"

	"open-cluster-management.io/ocm/pkg/addon"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/version"
)

// NewAddonManager generates a command to start addon manager
func NewAddonManager() *cobra.Command {
	opts := commonoptions.NewOptions()
	cmdConfig := opts.
		NewControllerCommandConfig("manager", version.Get(), addon.RunManager)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "manager"
	cmd.Short = "Start the Addon Manager"

	flags := cmd.Flags()
	opts.AddFlags(flags)

	return cmd
}
