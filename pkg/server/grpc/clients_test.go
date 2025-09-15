package grpc

import (
	"context"
	"testing"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/rest"
)

func TestNewClientsInitializesFields(t *testing.T) {
	t.Parallel()
	controllerContext := &controllercmd.ControllerContext{
		KubeConfig: &rest.Config{Host: "https://example.com"},
	}
	clients, err := NewClients(controllerContext)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if clients.KubeClient == nil || clients.ClusterClient == nil || clients.WorkClient == nil || clients.AddOnClient == nil {
		t.Fatal("expected all clientsets to be initialized")
	}
	if clients.KubeInformers == nil || clients.ClusterInformers == nil || clients.WorkInformers == nil || clients.AddOnInformers == nil {
		t.Fatal("expected all informer factories to be initialized")
	}
}

func TestNewClients_ConfigValidation(t *testing.T) {
	tests := []struct {
		name          string
		host          string
		expectError   bool
		expectClients bool
	}{
		{
			name:          "valid host",
			host:          "https://example.com",
			expectError:   false,
			expectClients: true,
		},
		{
			name:          "malformed host",
			host:          "://",
			expectError:   true,
			expectClients: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controllerContext := &controllercmd.ControllerContext{
				KubeConfig: &rest.Config{
					Host: tt.host,
				},
			}

			clients, err := NewClients(controllerContext)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if clients != nil {
					t.Error("Expected clients to be nil when error occurs")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if clients == nil {
					t.Error("Expected clients to be non-nil when no error occurs")
				}
			}
		})
	}
}

func TestClientsRunMethodSignature(t *testing.T) {
	t.Parallel()
	// Compile-time assertion: (*Clients).Run must be func(*Clients, context.Context)
	var _ func(*Clients, context.Context) = (*Clients).Run
}
