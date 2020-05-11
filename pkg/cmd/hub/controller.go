package hub

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/registration/pkg/hub"
	"github.com/open-cluster-management/registration/pkg/version"
)

func NewController() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("registration-controller", version.Get(), hub.RunControllerManager).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the Cluster Registration Controller"

	return cmd
}
