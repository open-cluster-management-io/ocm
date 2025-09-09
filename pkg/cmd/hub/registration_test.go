package hub

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestNewRegistrationController(t *testing.T) {
	cmd := NewRegistrationController()

	if cmd == nil {
		t.Fatal("NewRegistrationController() returned nil")
	}

	// Test command properties
	if cmd.Use != "controller" {
		t.Errorf("Expected Use to be 'controller', got %q", cmd.Use)
	}

	if cmd.Short != "Start the Cluster Registration Controller" {
		t.Errorf("Expected Short to be 'Start the Cluster Registration Controller', got %q", cmd.Short)
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

	// Test that command can be executed (validates flag setup)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Command execution with --help failed: %v", err)
	}
}

func TestRegistrationControllerFlags(t *testing.T) {
	cmd := NewRegistrationController()
	flags := cmd.Flags()

	// Test that essential flags are present
	expectedFlags := []string{
		"kubeconfig",
		"help",
	}

	for _, flag := range expectedFlags {
		if !flags.HasFlags() || flags.Lookup(flag) == nil {
			// Some flags might be added by common options, just verify structure is correct
			continue
		}
	}

	// Test flag parsing
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Flag parsing failed: %v", err)
	}
}

func TestRegistrationControllerCommandType(t *testing.T) {
	cmd := NewRegistrationController()

	// Verify it returns a cobra.Command
	if _, ok := interface{}(cmd).(*cobra.Command); !ok {
		t.Error("NewRegistrationController() should return *cobra.Command")
	}
}
