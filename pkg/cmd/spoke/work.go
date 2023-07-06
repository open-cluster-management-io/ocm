package spoke

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	ocmfeature "open-cluster-management.io/api/feature"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/version"
	"open-cluster-management.io/ocm/pkg/work/spoke"
)

// NewWorkAgent generates a command to start work agent
func NewWorkAgent() *cobra.Command {
	commonOptions := commonoptions.NewAgentOptions()
	agentOption := spoke.NewWorkloadAgentOptions()
	cfg := spoke.NewWorkAgentConfig(commonOptions, agentOption)
	cmdConfig := controllercmd.
		NewControllerCommandConfig("work-agent", version.Get(), cfg.RunWorkloadAgent)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = agentCmdName
	cmd.Short = "Start the Work Agent"

	// add disable leader election flag
	flags := cmd.Flags()
	commonOptions.AddFlags(flags)
	agentOption.AddFlags(flags)
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	features.SpokeMutableFeatureGate.AddFlag(flags)

	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election for the agent.")

	return cmd
}
