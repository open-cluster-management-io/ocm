package spoke

import (
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/component-base/featuregate"

	"open-cluster-management.io/ocm/pkg/features"
)

func TestNewWorkAgent(t *testing.T) {
	// Reset feature gate for this test
	old := features.SpokeMutableFeatureGate
	features.SpokeMutableFeatureGate = featuregate.NewFeatureGate()
	t.Cleanup(func() { features.SpokeMutableFeatureGate = old })

	cmd := NewWorkAgent()

	if cmd == nil {
		t.Fatal("NewWorkAgent() returned nil")
	}

	// Test command properties
	if cmd.Use != agentCmdName {
		t.Errorf("Expected Use to be %q, got %q", agentCmdName, cmd.Use)
	}

	if cmd.Short != "Start the Work Agent" {
		t.Errorf("Expected Short to be 'Start the Work Agent', got %q", cmd.Short)
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

func TestWorkAgentFlags(t *testing.T) {
	// Reset feature gate for this test
	old := features.SpokeMutableFeatureGate
	features.SpokeMutableFeatureGate = featuregate.NewFeatureGate()
	t.Cleanup(func() { features.SpokeMutableFeatureGate = old })

	cmd := NewWorkAgent()
	flags := cmd.Flags()

	// Test that essential flags are present (from common options and agent options)
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
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Flag parsing with --help failed: %v", err)
	}
}

func TestWorkAgentCommandType(t *testing.T) {
	// Reset feature gate for this test
	old := features.SpokeMutableFeatureGate
	features.SpokeMutableFeatureGate = featuregate.NewFeatureGate()
	t.Cleanup(func() { features.SpokeMutableFeatureGate = old })

	cmd := NewWorkAgent()

	// Verify it returns a cobra.Command
	if _, ok := interface{}(cmd).(*cobra.Command); !ok {
		t.Error("NewWorkAgent() should return *cobra.Command")
	}
}
