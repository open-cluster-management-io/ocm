package grpc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
)

func TestNewGRPCServerOptions(t *testing.T) {
	opts := NewGRPCServerOptions()

	if opts == nil {
		t.Fatal("NewGRPCServerOptions() returned nil")
	}

	if opts.GRPCServerConfig != "" {
		t.Errorf("Expected GRPCServerConfig to be empty by default, got %q", opts.GRPCServerConfig)
	}
}

func TestGRPCServerOptionsAddFlags(t *testing.T) {
	opts := NewGRPCServerOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	opts.AddFlags(fs)

	// Test that server-config flag is added
	serverConfigFlag := fs.Lookup("server-config")
	if serverConfigFlag == nil {
		t.Error("Expected server-config flag to be added")
	}

	// Test that flag has correct description
	if serverConfigFlag.Usage != "Location of the server configuration file." {
		t.Errorf("Expected flag usage to be 'Location of the server configuration file.', got %q", serverConfigFlag.Usage)
	}

	// Test setting the flag value
	err := fs.Set("server-config", "/path/to/config.yaml")
	if err != nil {
		t.Errorf("Failed to set server-config flag: %v", err)
	}

	if opts.GRPCServerConfig != "/path/to/config.yaml" {
		t.Errorf("Expected GRPCServerConfig to be '/path/to/config.yaml', got %q", opts.GRPCServerConfig)
	}
}

func TestGRPCServerOptionsStruct(t *testing.T) {
	opts := &GRPCServerOptions{
		GRPCServerConfig: "test-config.yaml",
	}

	if opts.GRPCServerConfig != "test-config.yaml" {
		t.Error("GRPCServerConfig field not set correctly")
	}
}

func TestGRPCServerOptionsFlagTypes(t *testing.T) {
	opts := NewGRPCServerOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	opts.AddFlags(fs)

	flag := fs.Lookup("server-config")
	if flag == nil {
		t.Fatal("server-config flag not found")
	}

	if flag.Value.Type() != "string" {
		t.Errorf("Expected server-config flag to be string type, got %q", flag.Value.Type())
	}
}

func TestGRPCServerOptionsRunWithInvalidConfig(t *testing.T) {
	opts := NewGRPCServerOptions()
	opts.GRPCServerConfig = "/nonexistent/path/to/config.yaml"

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig: &rest.Config{Host: "https://example.com"},
	}

	// This should return an error because the config file doesn't exist
	err := opts.Run(ctx, controllerContext)
	if err == nil {
		t.Error("Expected error when config file doesn't exist, but got none")
	}
}

func TestGRPCServerOptionsRunWithInvalidKubeConfig(t *testing.T) {
	opts := NewGRPCServerOptions()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Invalid kubeconfig that should cause client creation to fail
	controllerContext := &controllercmd.ControllerContext{
		KubeConfig: &rest.Config{Host: "://invalid-url"},
	}

	// This should return an error because the kubeconfig is invalid
	err := opts.Run(ctx, controllerContext)
	if err == nil {
		t.Error("Expected error when kubeconfig is invalid, but got none")
	}
}

func TestGRPCServerOptionsRunWithValidConfigFile(t *testing.T) {
	// Create a temporary config file for testing
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "grpc-config.yaml")

	// Create temporary certificate files
	certFile := filepath.Join(tempDir, "tls.crt")
	keyFile := filepath.Join(tempDir, "tls.key")

	// Create dummy certificate and key files
	err := os.WriteFile(certFile, []byte("dummy-cert"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test cert file: %v", err)
	}
	err = os.WriteFile(keyFile, []byte("dummy-key"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test key file: %v", err)
	}

	// Create a valid GRPC server config with certificate paths
	configContent := `
port: 0
grpc_certificate_file: ` + certFile + `
grpc_private_key_file: ` + keyFile + `
`
	err = os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	opts := NewGRPCServerOptions()
	opts.GRPCServerConfig = configFile

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	controllerContext := &controllercmd.ControllerContext{
		KubeConfig: &rest.Config{Host: "https://example.com"},
	}

	// This should try to start the server but will likely fail due to invalid certificates
	// We mainly want to test that it gets past the config loading phase
	err = opts.Run(ctx, controllerContext)

	// We expect this to fail, but it should be because of invalid certificates, not missing config
	if err == nil {
		t.Error("Expected error due to invalid certificates, but got none")
	}

	// The error should be related to certificate loading, not config file reading
	if err != nil && !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "tls") {
		t.Errorf("Expected certificate-related error, got: %v", err)
	}
}
