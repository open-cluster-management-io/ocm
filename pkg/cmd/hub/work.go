package hub

import (
	"context"

	"github.com/spf13/cobra"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/version"
	"open-cluster-management.io/ocm/pkg/work/hub"
)

// NewHubManager generates a command to start hub manager
func NewWorkController() *cobra.Command {
	opts := commonoptions.NewOptions()
	cmdConfig := opts.
		NewControllerCommandConfig("work-manager", version.Get(), hub.RunWorkHubManager)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "manager"
	cmd.Short = "Start the Work Hub Manager"

	return cmd
}
