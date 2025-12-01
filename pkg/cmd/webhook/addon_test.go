package webhook

import (
	"testing"
)

func TestNewAddonWebhook(t *testing.T) {
	cmd := NewAddonWebhook()
	if cmd == nil {
		t.Errorf("NewAddonWebhook() should not return nil")
	}
	if cmd.Use != "webhook-server" {
		t.Errorf("expected webhook-server, but got %s", cmd.Use)
	}
	if cmd.Short != "Start the addon conversion webhook server" {
		t.Errorf("expected 'Start the addon conversion webhook server', but got %s", cmd.Short)
	}
}
