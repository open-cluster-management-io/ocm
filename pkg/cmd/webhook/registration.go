package webhook

import (
	"github.com/spf13/cobra"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/webhook"
)

func NewRegistrationWebhook() *cobra.Command {
	ops := webhook.NewOptions()
	cmd := &cobra.Command{
		Use:   "webhook-server",
		Short: "Start the registration webhook server",
		RunE: func(c *cobra.Command, args []string) error {
			err := ops.RunWebhookServer()
			return err
		},
	}

	flags := cmd.Flags()
	ops.AddFlags(flags)

	features.DefaultHubRegistrationMutableFeatureGate.AddFlag(flags)
	return cmd
}
