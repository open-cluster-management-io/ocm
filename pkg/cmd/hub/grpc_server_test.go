package hub

import (
	"testing"

	"github.com/spf13/cobra"
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
