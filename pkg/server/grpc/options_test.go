package grpc

import (
	"testing"

	"github.com/spf13/pflag"
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
