package conversion

import (
	"github.com/spf13/cobra"
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
	return cmd
}
