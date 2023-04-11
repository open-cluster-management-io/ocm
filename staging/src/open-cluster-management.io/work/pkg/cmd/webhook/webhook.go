package webhook

import (
	"github.com/spf13/cobra"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	_ "open-cluster-management.io/work/pkg/features"
)

func NewWebhook() *cobra.Command {
	ops := NewOptions()
	cmd := &cobra.Command{
		Use:   "webhook-server",
		Short: "Start the webhook server",
		RunE: func(c *cobra.Command, args []string) error {
			err := ops.RunWebhookServer()
			return err
		},
	}

	flags := cmd.Flags()
	ops.AddFlags(flags)
	featureGate := utilfeature.DefaultMutableFeatureGate
	featureGate.AddFlag(flags)
	return cmd
}
