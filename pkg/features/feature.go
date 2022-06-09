package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	ocmfeature "open-cluster-management.io/api/feature"
)

var (
	// DefaultSpokeMutableFeatureGate is made up of multiple mutable feature-gates for registration agent.
	DefaultSpokeMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// DefaultHubMutableFeatureGate made up of multiple mutable feature-gates for registration hub controller.
	DefaultHubMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()
)

func init() {
	runtime.Must(DefaultSpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
	runtime.Must(DefaultHubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
}
