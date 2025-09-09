package hub

import (
	"io"
	"testing"
)

func TestNewHubOperatorCmd(t *testing.T) {
	cmd := NewHubOperatorCmd()

	if cmd == nil {
		t.Fatal("NewHubOperatorCmd() returned nil")
	}

	// Test command properties
	if cmd.Use != "hub" {
		t.Errorf("Expected Use to be 'hub', got %q", cmd.Use)
	}

	if cmd.Short != "Start the cluster manager operator" {
		t.Errorf("Expected Short to be 'Start the cluster manager operator', got %q", cmd.Short)
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

func TestHubOperatorFlags(t *testing.T) {
	cmd := NewHubOperatorCmd()
	flags := cmd.Flags()

	// Test that specific flags are present
	expectedFlags := map[string]string{
		"skip-remove-crds":                  "bool",
		"control-plane-node-label-selector": "string",
		"deployment-replicas":               "int32",
		"enable-sync-labels":                "bool",
	}

	for flagName, flagType := range expectedFlags {
		flag := flags.Lookup(flagName)
		if flag == nil {
			t.Errorf("Expected flag %q to be present", flagName)
			continue
		}

		switch flagType {
		case "bool":
			if flag.Value.Type() != "bool" {
				t.Errorf("Expected flag %q to be bool type, got %q", flagName, flag.Value.Type())
			}
		case "string":
			if flag.Value.Type() != "string" {
				t.Errorf("Expected flag %q to be string type, got %q", flagName, flag.Value.Type())
			}
		case "int32":
			if flag.Value.Type() != "int32" {
				t.Errorf("Expected flag %q to be int32 type, got %q", flagName, flag.Value.Type())
			}
		}
	}
}

func TestHubOperatorFlagDefaults(t *testing.T) {
	cmd := NewHubOperatorCmd()
	flags := cmd.Flags()

	// Test default values
	tests := []struct {
		flagName        string
		expectedDefault string
	}{
		{"skip-remove-crds", "false"},
		{"control-plane-node-label-selector", "node-role.kubernetes.io/master="},
		{"deployment-replicas", "0"},
		{"enable-sync-labels", "false"},
	}

	for _, test := range tests {
		flag := flags.Lookup(test.flagName)
		if flag == nil {
			t.Errorf("Flag %q not found", test.flagName)
			continue
		}

		if flag.DefValue != test.expectedDefault {
			t.Errorf("Flag %q default value: expected %q, got %q", test.flagName, test.expectedDefault, flag.DefValue)
		}
	}
}

func TestHubOperatorCommandExecution(t *testing.T) {
	cmd := NewHubOperatorCmd()

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
