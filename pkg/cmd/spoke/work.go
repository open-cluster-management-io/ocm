package spoke

import (
	"context"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/clock"

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
	cmdConfig := commonOptions.CommonOpts.
		NewControllerCommandConfig("work-agent", version.Get(), cfg.RunWorkloadAgent, clock.RealClock{})
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = agentCmdName
	cmd.Short = "Start the Work Agent"

	// add disable leader election flag
	flags := cmd.Flags()
	commonOptions.AddFlags(flags)
	agentOption.AddFlags(flags)
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	features.SpokeMutableFeatureGate.AddFlag(flags)

	return cmd
}
