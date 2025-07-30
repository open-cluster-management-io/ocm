package webhook

import (
	"testing"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
)

func TestSetupWebhookServer(t *testing.T) {
	opts := commonoptions.NewWebhookOptions()
	err := SetupWebhookServer(opts)
	if err != nil {
		t.Errorf("SetupWebhookServer() error = %v, wantErr %v", err, nil)
	}
}
