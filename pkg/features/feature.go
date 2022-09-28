// Copyright (c) Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	ocmfeature "open-cluster-management.io/api/feature"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates))
}
