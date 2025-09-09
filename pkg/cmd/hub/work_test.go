package hub

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestNewWorkController(t *testing.T) {
	cmd := NewWorkController()

	if cmd == nil {
		t.Fatal("NewWorkController() returned nil")
	}

	// Test command properties
	if cmd.Use != "manager" {
		t.Errorf("Expected Use to be 'manager', got %q", cmd.Use)
	}

	if cmd.Short != "Start the Work Hub Manager" {
		t.Errorf("Expected Short to be 'Start the Work Hub Manager', got %q", cmd.Short)
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

func TestWorkControllerFlags(t *testing.T) {
	cmd := NewWorkController()
	flags := cmd.Flags()

	// Test that essential flags are present (from common options and work hub options)
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

func TestWorkControllerCommandType(t *testing.T) {
	cmd := NewWorkController()

	// Verify it returns a cobra.Command
	if _, ok := interface{}(cmd).(*cobra.Command); !ok {
		t.Error("NewWorkController() should return *cobra.Command")
	}
}

func TestWorkControllerCommandExecution(t *testing.T) {
	cmd := NewWorkController()

	// Test command execution with help flag
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Command execution with --help failed: %v", err)
	}
}
