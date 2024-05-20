package hub

import (
	"context"

	"github.com/spf13/cobra"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager"
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
	flags.StringToStringVar(&cmOptions.MasterNodeLabelSelector, "master-node-label-selector",
		map[string]string{"node-role.kubernetes.io/master": ""}, "Label selector for master nodes, e.g. 'node-role.kubernetes.io/master='")
	flags.Int32Var(&cmOptions.DeploymentReplicas, "deployment-replicas", 0,
		"Number of deployment replicas, operator will automatically determine replicas if not set")
	opts.AddFlags(flags)
	return cmd
}
