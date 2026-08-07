package hub

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/utils/clock"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
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
	flags.BoolVar(&cmOptions.EnableSyncLabels, "enable-sync-labels", false,
		"If set, will sync the labels of ClusterManager CR to all hub resources")
	flags.StringVar(&cmOptions.ImagePullSecretName, "image-pull-secret-name", helpers.ImagePullSecret,
		"The name of the image pull secret in the operator namespace to sync to hub components")
	opts.AddFlags(flags)
	// The cluster-manager operator does not receive --tls-min-version /
	// --tls-cipher-suites flags from its deployment manifest (unlike the hub
	// controllers it manages). Instead, it reads the ocm-tls-profile ConfigMap
	// itself at runtime. ApplyTLSFromConfigMapToCommand installs a
	// PersistentPreRunE hook that reads that ConfigMap once before
	// library-go's StartController starts the health/metrics server (port 8443),
	// so the server uses the cluster TLS profile from the first request.
	// When the ConfigMap changes, RunClusterManagerOperator's watcher calls
	// os.Exit(0), Kubernetes restarts the pod, and this hook re-reads the
	// updated ConfigMap on the next startup.
	opts.ApplyTLSFromConfigMapToCommand(cmd)
	return cmd
}
