// Copyright (c) Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	ocmfeature "open-cluster-management.io/api/feature"
)

var (
	// DefaultHubWorkMutableFeatureGate is made up of multiple mutable feature-gates for work controller
	DefaultHubWorkMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// DefaultHubRegistrationMutableFeatureGate made up of multiple mutable feature-gates for registration hub controller.
	DefaultHubRegistrationMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// SpokeMutableFeatureGate of multiple mutable feature-gates for agent
	SpokeMutableFeatureGate = featuregate.NewFeatureGate()
)

func init() {
	runtime.Must(DefaultHubWorkMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates))
	runtime.Must(DefaultHubRegistrationMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
}
