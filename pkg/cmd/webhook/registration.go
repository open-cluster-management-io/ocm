package webhook

import (
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/webhook"
)

func NewRegistrationWebhook() *cobra.Command {
	webhookOptions := commonoptions.NewWebhookOptions()
	cmd := &cobra.Command{
		Use:   "webhook-server",
		Short: "Start the registration webhook server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := webhook.SetupWebhookServer(webhookOptions); err != nil {
				return err
			}
			return webhookOptions.RunWebhookServer(ctrl.SetupSignalHandler())
		},
	}

	flags := cmd.Flags()
	webhookOptions.AddFlags(flags)

	return cmd
}
