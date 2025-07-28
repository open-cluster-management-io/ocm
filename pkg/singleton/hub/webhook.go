package hub

import (
	"github.com/spf13/pflag"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registrationwebhook "open-cluster-management.io/ocm/pkg/registration/webhook"
	workwebhook "open-cluster-management.io/ocm/pkg/work/webhook"
)

// Config contains the server (the webhook) cert and key.
type WebhookOptions struct {
	workWebhookOptions *workwebhook.Options
}

// NewWebhookOptions constructs a new set of default options for webhook.
func NewWebhookOptions() *WebhookOptions {
	return &WebhookOptions{
		workWebhookOptions: workwebhook.NewOptions(),
	}
}

func (c *WebhookOptions) AddFlags(fs *pflag.FlagSet) {
	c.workWebhookOptions.AddFlags(fs)
}

func (c *WebhookOptions) SetupWebhookServer(opts *commonoptions.WebhookOptions) error {
	if err := registrationwebhook.SetupWebhookServer(opts); err != nil {
		return err
	}
	if err := c.workWebhookOptions.SetupWebhookServer(opts); err != nil {
		return err
	}

	return nil
}
