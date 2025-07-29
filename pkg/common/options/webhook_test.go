package options

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestNewWebhookOptions(t *testing.T) {
	opts := NewWebhookOptions()
	if opts.Port != 9443 {
		t.Errorf("expected port 9443, but got %d", opts.Port)
	}
	if opts.scheme == nil {
		t.Errorf("scheme should not be nil")
	}
}

func TestWebhookOptions_AddFlags(t *testing.T) {
	opts := NewWebhookOptions()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(flags)

	if err := flags.Parse([]string{"--port=8443", "--certdir=/tmp/certs"}); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	if opts.Port != 8443 {
		t.Errorf("expected port 8443, but got %d", opts.Port)
	}
	if opts.CertDir != "/tmp/certs" {
		t.Errorf("expected certdir /tmp/certs, but got %s", opts.CertDir)
	}
}

type testWebhookInitializer struct {
	initialized bool
}

func (t *testWebhookInitializer) Init(mgr ctrl.Manager) error {
	t.initialized = true
	return nil
}

func TestWebhookOptions_InstallWebhook(t *testing.T) {
	opts := NewWebhookOptions()
	initializer := &testWebhookInitializer{}
	opts.InstallWebhook(initializer)

	if len(opts.webhooks) != 1 {
		t.Errorf("expected 1 webhook, but got %d", len(opts.webhooks))
	}
}

func TestWebhookOptions_InstallScheme(t *testing.T) {
	opts := NewWebhookOptions()
	gv := schema.GroupVersion{Group: "testgroup", Version: "v1"}
	err := opts.InstallScheme(func(s *runtime.Scheme) error {
		s.AddKnownTypes(gv, &runtime.Unknown{})
		return nil
	})
	if err != nil {
		t.Fatalf("failed to install scheme: %v", err)
	}

	if !opts.scheme.Recognizes(gv.WithKind("Unknown")) {
		t.Errorf("scheme should recognize testgroup/v1, Kind=Unknown")
	}
}

func TestWebhookOptions_RunWebhookServer(t *testing.T) {
	testEnv := &envtest.Environment{}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("Failed to start test environment: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("Failed to stop test environment: %v", err)
		}
	}()

	opts := NewWebhookOptions()
	opts.Port = 9444 // Use a different port for webhook server
	opts.cfg = cfg

	initializer := &testWebhookInitializer{}
	opts.InstallWebhook(initializer)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	var serverErr error
	go func() {
		defer wg.Done()
		serverErr = opts.RunWebhookServer(ctx)
	}()

	// Wait for health check to be ready
	healthzURL := "http://localhost:8000/healthz"
	err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		client := http.Client{Timeout: 1 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", healthzURL, nil)
		if err != nil {
			t.Logf("failed to create request: %v", err)
			return false, nil // continue polling
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("Health check not ready yet, err: %v", err)
			return false, nil // continue polling
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true, nil // condition met
		}
		return false, nil // continue polling
	})

	if err != nil {
		t.Fatalf("Webhook server health check did not return 200 OK within the time limit: %v", err)
	}

	if !initializer.initialized {
		t.Error("Webhook initializer was not called")
	}

	cancel()
	wg.Wait()

	if serverErr != nil && serverErr != context.Canceled && serverErr != context.DeadlineExceeded {
		t.Errorf("RunWebhookServer returned an unexpected error: %v", serverErr)
	}
}
