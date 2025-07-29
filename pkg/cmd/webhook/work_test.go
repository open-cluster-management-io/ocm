package webhook

import (
	"testing"
)

func TestNewWorkWebhook(t *testing.T) {
	cmd := NewWorkWebhook()
	if cmd == nil {
		t.Errorf("NewWorkWebhook() should not return nil")
	}
	if cmd.Use != "webhook-server" {
		t.Errorf("expected webhook-server, but got %s", cmd.Use)
	}
	if cmd.Short != "Start the work webhook server" {
		t.Errorf("expected 'Start the work webhook server', but got %s", cmd.Short)
	}
}
