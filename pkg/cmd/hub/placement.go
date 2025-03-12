package hub

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/utils/clock"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewPlacementController() *cobra.Command {
	opts := commonoptions.NewOptions()
	cmdConfig := opts.
		NewControllerCommandConfig("placement", version.Get(), controllers.RunControllerManager, clock.RealClock{})
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "controller"
	cmd.Short = "Start the Placement Scheduling Controller"

	flags := cmd.Flags()
	opts.AddFlags(flags)

	return cmd
}
