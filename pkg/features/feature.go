// Copyright Contributors to the Open Cluster Management project

package features

import (
	"k8s.io/component-base/featuregate"
)

var (
	// HubMutableFeatureGate of multiple mutable feature-gate for hub
	HubMutableFeatureGate = featuregate.NewFeatureGate()

	// SpokeMutableFeatureGate of multiple mutable feature-gates for agent
	SpokeMutableFeatureGate = featuregate.NewFeatureGate()
)
