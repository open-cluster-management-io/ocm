package webhook

import (
	"testing"
)

func TestNewRegistrationWebhook(t *testing.T) {
	cmd := NewRegistrationWebhook()
	if cmd == nil {
		t.Errorf("NewRegistrationWebhook() should not return nil")
	}
	if cmd.Use != "webhook-server" {
		t.Errorf("expected webhook-server, but got %s", cmd.Use)
	}
	if cmd.Short != "Start the registration webhook server" {
		t.Errorf("expected 'Start the registration webhook server', but got %s", cmd.Short)
	}
}
