// Copyright (c) Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	ocmfeature "open-cluster-management.io/api/feature"
)

var (
	// DefaultSpokeMutableFeatureGate is made up of multiple mutable feature-gates for work agent.
	DefaultSpokeMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()
)

func init() {
	runtime.Must(DefaultSpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates))
}
