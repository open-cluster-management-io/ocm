package spoke

import (
	"context"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	ocmfeature "open-cluster-management.io/api/feature"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet"
	registration "open-cluster-management.io/ocm/pkg/registration/spoke"
	singletonspoke "open-cluster-management.io/ocm/pkg/singleton/spoke"
	"open-cluster-management.io/ocm/pkg/version"
	work "open-cluster-management.io/ocm/pkg/work/spoke"
)

const agentCmdName = "agent"

// NewKlusterletOperatorCmd generate a command to start klusterlet operator
func NewKlusterletOperatorCmd() *cobra.Command {
	opts := commonoptions.NewOptions()
	klOptions := klusterlet.Options{}
	cmdConfig := opts.
		NewControllerCommandConfig("klusterlet", version.Get(), klOptions.RunKlusterletOperator)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "klusterlet"
	cmd.Short = "Start the klusterlet operator"

	// add disable leader election flag
	flags := cmd.Flags()
	cmd.Flags().BoolVar(&klOptions.SkipPlaceholderHubSecret, "skip-placeholder-hub-secret", false,
		"If set, will skip ensuring a placeholder hub secret which is originally intended for pulling "+
			"work image before approved")
	opts.AddFlags(flags)

	return cmd
}

// NewKlusterletAgentCmd is to start the singleton agent including registration/work
func NewKlusterletAgentCmd() *cobra.Command {
	commonOptions := commonoptions.NewAgentOptions()
	workOptions := work.NewWorkloadAgentOptions()
	registrationOption := registration.NewSpokeAgentOptions()

	agentConfig := singletonspoke.NewAgentConfig(commonOptions, registrationOption, workOptions)
	cmdConfig := commonOptions.CommoOpts.
		NewControllerCommandConfig("klusterlet", version.Get(), agentConfig.RunSpokeAgent)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = agentCmdName
	cmd.Short = "Start the klusterlet agent"

	flags := cmd.Flags()

	commonOptions.AddFlags(flags)
	workOptions.AddFlags(flags)
	registrationOption.AddFlags(flags)

	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	features.SpokeMutableFeatureGate.AddFlag(flags)
	return cmd
}
