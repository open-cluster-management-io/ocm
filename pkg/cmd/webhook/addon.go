// Copyright Contributors to the Open Cluster Management project

package webhook

import (
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"open-cluster-management.io/ocm/pkg/addon/webhook"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
)

// NewAddonWebhook creates a new addon webhook server command
func NewAddonWebhook() *cobra.Command {
	webhookOptions := commonoptions.NewWebhookOptions()
	cmd := &cobra.Command{
		Use:   "webhook-server",
		Short: "Start the addon conversion webhook server",
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
