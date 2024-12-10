package hub

import (
	"context"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	ocmfeature "open-cluster-management.io/api/feature"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager"
	singletonhub "open-cluster-management.io/ocm/pkg/singleton/hub"
	"open-cluster-management.io/ocm/pkg/version"
)

// NewHubOperatorCmd generatee a command to start hub operator
func NewHubOperatorCmd() *cobra.Command {
	opts := commonoptions.NewOptions()
	cmOptions := clustermanager.Options{}
	cmd := opts.
		NewControllerCommandConfig("clustermanager", version.Get(), cmOptions.RunClusterManagerOperator).
		NewCommandWithContext(context.TODO())
	cmd.Use = "hub"
	cmd.Short = "Start the cluster manager operator"

	flags := cmd.Flags()
	flags.BoolVar(&cmOptions.SkipRemoveCRDs, "skip-remove-crds", false, "Skip removing CRDs while ClusterManager is deleting.")
	flags.StringVar(&cmOptions.ControlPlaneNodeLabelSelector, "control-plane-node-label-selector",
		"node-role.kubernetes.io/master=", "control plane node labels, "+
			"e.g. 'environment=production', 'tier notin (frontend,backend)'")
	flags.Int32Var(&cmOptions.DeploymentReplicas, "deployment-replicas", 0,
		"Number of deployment replicas, operator will automatically determine replicas if not set")
	opts.AddFlags(flags)
	return cmd
}

// NewHubManagerCmd is to start the singleton manager including registration/work/placement/addon
func NewHubManagerCmd() *cobra.Command {
	opts := commonoptions.NewOptions()
	hubOpts := singletonhub.NewOption()

	cmdConfig := opts.
		NewControllerCommandConfig("ocm-controller", version.Get(), hubOpts.RunControllerManager)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "manager"
	cmd.Short = "Start the ocm manager"

	flags := cmd.Flags()

	opts.AddFlags(flags)

	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates))
	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubAddonManagerFeatureGates))
	features.HubMutableFeatureGate.AddFlag(flags)
	return cmd
}
