package features

import (
	"testing"

	"k8s.io/component-base/featuregate"
)

func TestHubMutableFeatureGate(t *testing.T) {
	if HubMutableFeatureGate == nil {
		t.Error("HubMutableFeatureGate should not be nil")
	}

	// Test that it's a proper feature gate
	if _, ok := interface{}(HubMutableFeatureGate).(featuregate.MutableFeatureGate); !ok {
		t.Error("HubMutableFeatureGate should implement MutableFeatureGate interface")
	}
}

func TestSpokeMutableFeatureGate(t *testing.T) {
	if SpokeMutableFeatureGate == nil {
		t.Error("SpokeMutableFeatureGate should not be nil")
	}

	// Test that it's a proper feature gate
	if _, ok := interface{}(SpokeMutableFeatureGate).(featuregate.MutableFeatureGate); !ok {
		t.Error("SpokeMutableFeatureGate should implement MutableFeatureGate interface")
	}
}

func TestFeatureGatesAreDifferent(t *testing.T) {
	if HubMutableFeatureGate == SpokeMutableFeatureGate {
		t.Error("HubMutableFeatureGate and SpokeMutableFeatureGate should be different instances")
	}
}
