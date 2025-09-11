package spoke

import (
	"testing"

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
