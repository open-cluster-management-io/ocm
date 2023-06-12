package spoke

import (
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"

	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet"
	"open-cluster-management.io/ocm/pkg/version"
)

// NewKlusterletOperatorCmd generatee a command to start klusterlet operator
func NewKlusterletOperatorCmd() *cobra.Command {

	options := klusterlet.Options{}
	cmdConfig := controllercmd.
		NewControllerCommandConfig("klusterlet", version.Get(), options.RunKlusterletOperator)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "klusterlet"
	cmd.Short = "Start the klusterlet operator"

	// add disable leader election flag
	cmd.Flags().BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the agent.")
	cmd.Flags().BoolVar(&options.SkipPlaceholderHubSecret, "skip-placeholder-hub-secret", false,
		"If set, will skip ensuring a placeholder hub secret which is originally intended for pulling "+
			"work image before approved")

	return cmd
}
