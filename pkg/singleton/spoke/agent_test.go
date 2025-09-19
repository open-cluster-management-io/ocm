package spoke

import (
	"context"
	"testing"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registration "open-cluster-management.io/ocm/pkg/registration/spoke"
	work "open-cluster-management.io/ocm/pkg/work/spoke"
)

func TestNewAgentConfig(t *testing.T) {
	agentOption := commonoptions.NewAgentOptions()
	registrationOption := registration.NewSpokeAgentOptions()
	workOption := work.NewWorkloadAgentOptions()

	// Create a simple cancel function
	cancel := func() {}

	config := NewAgentConfig(agentOption, registrationOption, workOption, cancel)

	if config == nil {
		t.Fatal("NewAgentConfig() returned nil")
	}

	if config.registrationConfig == nil {
		t.Error("Expected registrationConfig to be initialized")
	}

	if config.workConfig == nil {
		t.Error("Expected workConfig to be initialized")
	}
}

func TestAgentConfigStruct(t *testing.T) {
	config := &AgentConfig{}

	if config.registrationConfig != nil {
		t.Error("Expected registrationConfig to be nil initially")
	}

	if config.workConfig != nil {
		t.Error("Expected workConfig to be nil initially")
	}
}

func TestAgentConfigHealthCheckers(t *testing.T) {
	agentOption := commonoptions.NewAgentOptions()
	registrationOption := registration.NewSpokeAgentOptions()
	workOption := work.NewWorkloadAgentOptions()
	cancel := func() {}

	config := NewAgentConfig(agentOption, registrationOption, workOption, cancel)

	// Test that HealthCheckers method exists and can be called
	healthCheckers := config.HealthCheckers()

	// The result might be nil or empty, but the method should not panic
	if healthCheckers == nil {
		// This is acceptable - health checkers might not be set up
	} else if len(healthCheckers) < 0 {
		t.Error("Length should not be negative")
	}
}

func TestAgentConfigWithNilInputs(t *testing.T) {
	// Test with nil inputs to ensure no immediate panic
	config := NewAgentConfig(nil, nil, nil, nil)

	if config == nil {
		t.Fatal("NewAgentConfig() returned nil even with nil inputs")
	}

	// The internal configs might be nil or might handle nil inputs gracefully
	// Just verify the struct was created
}

func TestRunSpokeAgentSignature(t *testing.T) {
	// Test that RunSpokeAgent has the correct signature and can be called
	// This is a compile-time assertion that validates the method signature
	var _ func(*AgentConfig, context.Context, *controllercmd.ControllerContext) error = (*AgentConfig).RunSpokeAgent

	// Test that the config struct is properly set up for RunSpokeAgent
	agentOption := commonoptions.NewAgentOptions()
	registrationOption := registration.NewSpokeAgentOptions()
	workOption := work.NewWorkloadAgentOptions()
	cancel := func() {}

	config := NewAgentConfig(agentOption, registrationOption, workOption, cancel)

	// Just verify the config was created - the compile-time assertion above
	// ensures the method signature is correct
	if config == nil {
		t.Error("Config should not be nil")
	}
}

func TestAgentConfigFields(t *testing.T) {
	// Test the struct fields and configuration setup
	agentOption := commonoptions.NewAgentOptions()
	registrationOption := registration.NewSpokeAgentOptions()
	workOption := work.NewWorkloadAgentOptions()
	cancel := func() {}

	config := NewAgentConfig(agentOption, registrationOption, workOption, cancel)

	// Test that the configuration was set up properly
	if config.registrationConfig == nil {
		t.Error("registrationConfig should not be nil")
	}

	if config.workConfig == nil {
		t.Error("workConfig should not be nil")
	}

	// Test HealthCheckers method delegates to registration config
	healthCheckers := config.HealthCheckers()
	// This should not panic and should return the same as the registration config
	registrationHealthCheckers := config.registrationConfig.HealthCheckers()

	if len(healthCheckers) != len(registrationHealthCheckers) {
		t.Errorf("HealthCheckers delegation failed: expected %d, got %d",
			len(registrationHealthCheckers), len(healthCheckers))
	}
}
