package hub

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/utils/clock"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager"
	"open-cluster-management.io/ocm/pkg/version"
)

// NewHubOperatorCmd generate a command to start hub operator
func NewHubOperatorCmd() *cobra.Command {
	opts := commonoptions.NewOptions()
	cmOptions := clustermanager.Options{}
	cmd := opts.
		NewControllerCommandConfig("clustermanager", version.Get(), cmOptions.RunClusterManagerOperator, clock.RealClock{}).
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
