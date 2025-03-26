package spoke

import (
	"context"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/clock"

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
		NewControllerCommandConfig("klusterlet", version.Get(), klOptions.RunKlusterletOperator, clock.RealClock{})
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "klusterlet"
	cmd.Short = "Start the klusterlet operator"

	// add disable leader election flag
	flags := cmd.Flags()
	flags.BoolVar(&klOptions.SkipPlaceholderHubSecret, "skip-placeholder-hub-secret", false,
		"If set, will skip ensuring a placeholder hub secret which is originally intended for pulling "+
			"work image before approved")
	if err := flags.MarkDeprecated("skip-placeholder-hub-secret", "flag is not used in the operator."); err != nil {
		utilruntime.Must(err)
	}
	flags.StringVar(&klOptions.ControlPlaneNodeLabelSelector, "control-plane-node-label-selector",
		"node-role.kubernetes.io/master=", "control plane node labels, "+
			"e.g. 'environment=production', 'tier notin (frontend,backend)'")
	flags.Int32Var(&klOptions.DeploymentReplicas, "deployment-replicas", 0,
		"Number of deployment replicas, operator will automatically determine replicas if not set")
	flags.BoolVar(&klOptions.DisableAddonNamespace, "disable-default-addon-namespace", false,
		"If set, will not create default open-cluster-management-agent-addon ns")

	flags.BoolVar(&klOptions.EnableSyncLabels, "enable-sync-labels", false,
		"If set, will sync the labels of Klusterlet CR to all agent resources")

	opts.AddFlags(flags)

	return cmd
}

// NewKlusterletAgentCmd is to start the singleton agent including registration/work
func NewKlusterletAgentCmd() *cobra.Command {
	ctx, cancel := context.WithCancel(context.TODO())
	commonOptions := commonoptions.NewAgentOptions()
	workOptions := work.NewWorkloadAgentOptions()
	registrationOption := registration.NewSpokeAgentOptions()

	agentConfig := singletonspoke.NewAgentConfig(commonOptions, registrationOption, workOptions, cancel)
	cmdConfig := commonOptions.CommonOpts.
		NewControllerCommandConfig("klusterlet-agent", version.Get(), agentConfig.RunSpokeAgent, clock.RealClock{}).
		WithHealthChecks(agentConfig.HealthCheckers()...)
	cmd := cmdConfig.NewCommandWithContext(ctx)
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
