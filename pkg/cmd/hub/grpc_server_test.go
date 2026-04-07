package hub

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	grpcopts "open-cluster-management.io/ocm/pkg/server/grpc"
)

func TestNewGRPCServerCommand(t *testing.T) {
	cmd := NewGRPCServerCommand()

	if cmd == nil {
		t.Fatal("NewGRPCServerCommand() returned nil")
	}

	// Test command properties
	if cmd.Use != "grpc" {
		t.Errorf("Expected Use to be 'grpc', got %q", cmd.Use)
	}

	if cmd.Short != "Start the gRPC Server" {
		t.Errorf("Expected Short to be 'Start the gRPC Server', got %q", cmd.Short)
	}

	// Test that flags are added
	flags := cmd.Flags()
	if flags == nil {
		t.Error("Expected flags to be set")
	}

	// Verify command is runnable (has RunE or Run set)
	if cmd.RunE == nil && cmd.Run == nil {
		t.Error("Expected command to have RunE or Run set")
	}
}

func TestGRPCServerCommandFlags(t *testing.T) {
	cmd := NewGRPCServerCommand()
	flags := cmd.Flags()

	// Test that essential flags are present (from common options and grpc server options)
	if !flags.HasFlags() {
		t.Error("Expected command to have flags")
	}

	// Test flag parsing with help
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Flag parsing with --help failed: %v", err)
	}
}

func TestGRPCServerCommandType(t *testing.T) {
	cmd := NewGRPCServerCommand()

	// Verify it returns a cobra.Command
	if _, ok := interface{}(cmd).(*cobra.Command); !ok {
		t.Error("NewGRPCServerCommand() should return *cobra.Command")
	}
}

func TestGRPCServerCommandExecution(t *testing.T) {
	cmd := NewGRPCServerCommand()

	// Test command execution with help flag
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Command execution with --help failed: %v", err)
	}
}

func TestGRPCStartFuncBridgesTLSFlags(t *testing.T) {
	opts := commonoptions.NewOptions()
	opts.TLSMinVersion = "VersionTLS12"
	opts.TLSCipherSuites = "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"

	grpcServerOpts := grpcopts.NewGRPCServerOptions()
	startFunc := grpcStartFunc(opts, grpcServerOpts)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Run will fail (no real kube context), but the TLS fields should be bridged first.
	err := startFunc(ctx, &controllercmd.ControllerContext{
		KubeConfig: &rest.Config{Host: "https://example.com"},
	})

	// Verify TLS flags were bridged before Run failed
	if grpcServerOpts.TLSMinVersionOverride != "VersionTLS12" {
		t.Errorf("expected TLSMinVersionOverride=VersionTLS12, got %q", grpcServerOpts.TLSMinVersionOverride)
	}
	if grpcServerOpts.TLSCipherSuitesOverride != "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256" {
		t.Errorf("expected TLSCipherSuitesOverride=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, got %q",
			grpcServerOpts.TLSCipherSuitesOverride)
	}

	// The error should be from client creation, not TLS parsing
	if err != nil && strings.Contains(err.Error(), "failed to apply gRPC TLS overrides") {
		t.Errorf("unexpected TLS error: %v", err)
	}
}

func TestGRPCServerCommandTLSWiring(t *testing.T) {
	cmd := NewGRPCServerCommand()

	// ApplyTLSToCommand should have set PersistentPreRunE
	if cmd.PersistentPreRunE == nil {
		t.Error("Expected PersistentPreRunE to be set by ApplyTLSToCommand")
	}

	// TLS flags should be registered via common options
	flags := cmd.Flags()
	if flags.Lookup("tls-min-version") == nil {
		t.Error("Expected --tls-min-version flag to be registered")
	}
	if flags.Lookup("tls-cipher-suites") == nil {
		t.Error("Expected --tls-cipher-suites flag to be registered")
	}
}
