package hub

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"open-cluster-management.io/registration/pkg/hub"
	"open-cluster-management.io/registration/pkg/version"
)

func NewController() *cobra.Command {
	manager := hub.NewHubManagerOptions()
	cmdConfig := controllercmd.
		NewControllerCommandConfig("registration-controller", version.Get(), manager.RunControllerManager)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the Cluster Registration Controller"

	flags := cmd.Flags()

	flags.DurationVar(&cmdConfig.LeaseDuration.Duration, "leader-election-lease-duration", 137*time.Second, ""+
		"The duration that non-leader candidates will wait after observing a leadership "+
		"renewal until attempting to acquire leadership of a led but unrenewed leader "+
		"slot. This is effectively the maximum duration that a leader can be stopped "+
		"before it is replaced by another candidate. This is only applicable if leader "+
		"election is enabled.")
	flags.DurationVar(&cmdConfig.RenewDeadline.Duration, "leader-election-renew-deadline", 107*time.Second, ""+
		"The interval between attempts by the acting master to renew a leadership slot "+
		"before it stops leading. This must be less than or equal to the lease duration. "+
		"This is only applicable if leader election is enabled.")
	flags.DurationVar(&cmdConfig.RetryPeriod.Duration, "leader-election-retry-period", 26*time.Second, ""+
		"The duration the clients should wait between attempting acquisition and renewal "+
		"of a leadership. This is only applicable if leader election is enabled.")

	manager.AddFlags(cmd.Flags())

	return cmd
}
