package hub

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/utils/clock"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewRegistrationController() *cobra.Command {
	opts := commonoptions.NewOptions()
	manager := hub.NewHubManagerOptions()
	cmdConfig := opts.
		NewControllerCommandConfig("registration-controller", version.Get(), manager.RunControllerManager, clock.RealClock{})
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "controller"
	cmd.Short = "Start the Cluster Registration Controller"

	flags := cmd.Flags()
	manager.AddFlags(flags)
	opts.AddFlags(flags)

	return cmd
}
