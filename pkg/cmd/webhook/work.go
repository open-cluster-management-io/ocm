package webhook

import (
	"github.com/spf13/cobra"
	"open-cluster-management.io/ocm/pkg/features"
	_ "open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/webhook"
)

func NewWorkWebhook() *cobra.Command {
	ops := webhook.NewOptions()
	cmd := &cobra.Command{
		Use:   "webhook-server",
		Short: "Start the work webhook server",
		RunE: func(c *cobra.Command, args []string) error {
			err := ops.RunWebhookServer()
			return err
		},
	}

	flags := cmd.Flags()
	ops.AddFlags(flags)

	features.DefaultHubWorkMutableFeatureGate.AddFlag(flags)

	return cmd
}
