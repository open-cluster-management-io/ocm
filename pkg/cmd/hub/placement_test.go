package hub

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestNewPlacementController(t *testing.T) {
	cmd := NewPlacementController()

	if cmd == nil {
		t.Fatal("NewPlacementController() returned nil")
	}

	// Test command properties
	if cmd.Use != "controller" {
		t.Errorf("Expected Use to be 'controller', got %q", cmd.Use)
	}

	if cmd.Short != "Start the Placement Scheduling Controller" {
		t.Errorf("Expected Short to be 'Start the Placement Scheduling Controller', got %q", cmd.Short)
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

func TestPlacementControllerFlags(t *testing.T) {
	cmd := NewPlacementController()
	flags := cmd.Flags()

	// Test that essential flags are present (from common options)
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

func TestPlacementControllerCommandType(t *testing.T) {
	cmd := NewPlacementController()

	// Verify it returns a cobra.Command
	if _, ok := interface{}(cmd).(*cobra.Command); !ok {
		t.Error("NewPlacementController() should return *cobra.Command")
	}
}

func TestPlacementControllerCommandExecution(t *testing.T) {
	cmd := NewPlacementController()

	// Test command execution with help flag
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Command execution with --help failed: %v", err)
	}
}
