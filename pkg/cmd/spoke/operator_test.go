package spoke

import (
	"io"
	"testing"

	"k8s.io/component-base/featuregate"

	"open-cluster-management.io/ocm/pkg/features"
)

func TestAgentCmdName(t *testing.T) {
	// Test the constant
	if agentCmdName != "agent" {
		t.Errorf("Expected agentCmdName to be 'agent', got %q", agentCmdName)
	}
}

func TestNewKlusterletOperatorCmd(t *testing.T) {
	cmd := NewKlusterletOperatorCmd()

	if cmd == nil {
		t.Fatal("NewKlusterletOperatorCmd() returned nil")
	}

	// Test command properties
	if cmd.Use != "klusterlet" {
		t.Errorf("Expected Use to be 'klusterlet', got %q", cmd.Use)
	}

	if cmd.Short != "Start the klusterlet operator" {
		t.Errorf("Expected Short to be 'Start the klusterlet operator', got %q", cmd.Short)
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

func TestKlusterletOperatorFlags(t *testing.T) {
	cmd := NewKlusterletOperatorCmd()
	flags := cmd.Flags()

	// Test that specific flags are present
	expectedFlags := map[string]string{
		"skip-placeholder-hub-secret":       "bool",
		"control-plane-node-label-selector": "string",
		"deployment-replicas":               "int32",
		"disable-default-addon-namespace":   "bool",
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

func TestKlusterletOperatorDeprecatedFlag(t *testing.T) {
	cmd := NewKlusterletOperatorCmd()
	flags := cmd.Flags()

	// Test that deprecated flag exists and is marked as deprecated
	flag := flags.Lookup("skip-placeholder-hub-secret")
	if flag == nil {
		t.Error("Expected deprecated flag 'skip-placeholder-hub-secret' to be present")
		return
	}

	// In cobra, deprecated flags are still present but marked
	if flag.Deprecated == "" {
		t.Error("Expected 'skip-placeholder-hub-secret' flag to be marked as deprecated")
	}
}

func TestNewKlusterletAgentCmd(t *testing.T) {
	// Reset feature gate for this test
	old := features.SpokeMutableFeatureGate
	features.SpokeMutableFeatureGate = featuregate.NewFeatureGate()
	t.Cleanup(func() { features.SpokeMutableFeatureGate = old })

	cmd := NewKlusterletAgentCmd()

	if cmd == nil {
		t.Fatal("NewKlusterletAgentCmd() returned nil")
	}

	// Test command properties
	if cmd.Use != agentCmdName {
		t.Errorf("Expected Use to be %q, got %q", agentCmdName, cmd.Use)
	}

	if cmd.Short != "Start the klusterlet agent" {
		t.Errorf("Expected Short to be 'Start the klusterlet agent', got %q", cmd.Short)
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

func TestKlusterletAgentFlags(t *testing.T) {
	// Reset feature gate for this test
	old := features.SpokeMutableFeatureGate
	features.SpokeMutableFeatureGate = featuregate.NewFeatureGate()
	t.Cleanup(func() { features.SpokeMutableFeatureGate = old })

	cmd := NewKlusterletAgentCmd()
	flags := cmd.Flags()

	// Test that essential flags are present
	if !flags.HasFlags() {
		t.Error("Expected command to have flags")
	}

	// Check for feature gate flag
	featureGateFlag := flags.Lookup("feature-gates")
	if featureGateFlag == nil {
		t.Error("Expected feature-gates flag to be present")
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
