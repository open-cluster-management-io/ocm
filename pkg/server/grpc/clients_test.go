package grpc

import (
	"context"
	"testing"
	"time"

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

func TestClientsRun(t *testing.T) {
	t.Parallel()

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig: &rest.Config{Host: "https://example.com"},
	}

	clients, err := NewClients(controllerContext)
	if err != nil {
		t.Fatalf("expected no error creating clients, got %v", err)
	}

	// Create a context that we can cancel to test that Run respects context cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Start the clients in a goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		clients.Run(ctx)
	}()

	// Cancel the context to stop the informers
	cancel()

	// Verify that Run exits when context is canceled
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer timeoutCancel()

	select {
	case <-done:
		// Run method completed as expected
	case <-timeoutCtx.Done():
		t.Error("Run method did not exit within timeout after context cancellation")
	}
}

func TestClientsRunWithNilClients(t *testing.T) {
	// Test that Run panics when called on clients struct with nil informers
	// This demonstrates the expected behavior when informers are not properly initialized
	clients := &Clients{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// We expect this to panic due to nil informers
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when informers are nil, but got none")
		}
	}()

	clients.Run(ctx)
}
