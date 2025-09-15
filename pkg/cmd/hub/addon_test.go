package hub

import (
	"io"
	"testing"
)

func TestNewAddonManager(t *testing.T) {
	cmd := NewAddonManager()

	if cmd == nil {
		t.Fatal("NewAddonManager() returned nil")
	}

	// Test command properties
	if cmd.Use != "manager" {
		t.Errorf("Expected Use to be 'manager', got %q", cmd.Use)
	}

	if cmd.Short != "Start the Addon Manager" {
		t.Errorf("Expected Short to be 'Start the Addon Manager', got %q", cmd.Short)
	}

	// Basic sanity: the command exposes at least one flag
	if !cmd.Flags().HasFlags() {
		t.Error("Expected command to have flags")
	}

	// Verify command is runnable (has RunE or Run set)
	if cmd.RunE == nil && cmd.Run == nil {
		t.Error("Expected command to have RunE or Run set")
	}
}

func TestAddonManagerFlags(t *testing.T) {
	cmd := NewAddonManager()
	flags := cmd.Flags()

	// Test that essential flags are present (from common options)
	if !flags.HasFlags() {
		t.Error("Expected command to have flags")
	}

	// Test flag parsing with help
	cmd.SetArgs([]string{"--help"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Flag parsing with --help failed: %v", err)
	}
}

func TestAddonManagerCommandExecution(t *testing.T) {
	cmd := NewAddonManager()

	// Test command execution with help flag
	cmd.SetArgs([]string{"--help"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Command execution with --help failed: %v", err)
	}
}
