package webhook

import (
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	_ "open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/webhook"
)

func NewWorkWebhook() *cobra.Command {
	webhookOptions := commonoptions.NewWebhookOptions()
	opts := webhook.NewOptions()
	cmd := &cobra.Command{
		Use:   "webhook-server",
		Short: "Start the work webhook server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := opts.SetupWebhookServer(webhookOptions); err != nil {
				return err
			}
			return webhookOptions.RunWebhookServer(ctrl.SetupSignalHandler())
		},
	}

	flags := cmd.Flags()
	webhookOptions.AddFlags(flags)
	opts.AddFlags(flags)

	return cmd
}
