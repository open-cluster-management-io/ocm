package webhook

import (
	"github.com/spf13/cobra"
	"open-cluster-management.io/ocm/pkg/registration/features"
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

	features.DefaultHubMutableFeatureGate.AddFlag(flags)
	return cmd
}
