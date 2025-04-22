package spoke

import (
	"context"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/clock"

	ocmfeature "open-cluster-management.io/api/feature"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewRegistrationAgent() *cobra.Command {
	ctx, cancel := context.WithCancel(context.TODO())
	agentOptions := spoke.NewSpokeAgentOptions()
	commonOptions := commonoptions.NewAgentOptions()
	cfg := spoke.NewSpokeAgentConfig(commonOptions, agentOptions, cancel)
	cmdConfig := commonOptions.CommonOpts.
		NewControllerCommandConfig("registration-agent", version.Get(), cfg.RunSpokeAgent, clock.RealClock{}).
		WithHealthChecks(cfg.HealthCheckers()...)

	cmd := cmdConfig.NewCommandWithContext(ctx)
	cmd.Use = agentCmdName
	cmd.Short = "Start the Cluster Registration Agent"

	flags := cmd.Flags()
	commonOptions.AddFlags(flags)
	agentOptions.AddFlags(flags)

	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
	features.SpokeMutableFeatureGate.AddFlag(flags)
	return cmd
}
