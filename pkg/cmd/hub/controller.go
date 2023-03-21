package hub

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/hub"
	"open-cluster-management.io/registration/pkg/version"
)

func NewController() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("registration-controller", version.Get(), hub.RunControllerManager).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the Cluster Registration Controller"

	flags := cmd.Flags()
	features.DefaultHubMutableFeatureGate.AddFlag(flags)

	return cmd
}
