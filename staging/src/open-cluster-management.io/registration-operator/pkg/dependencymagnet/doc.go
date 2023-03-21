//go:build tools
// +build tools

// go mod won't pull in code that isn't depended upon, but we have some code we don't depend on from code that must be included
// for our build to work.
package dependencymagnet

import (
	_ "github.com/openshift/build-machinery-go"
	_ "open-cluster-management.io/api/addon/v1alpha1"
	_ "open-cluster-management.io/api/cluster/v1"
	_ "open-cluster-management.io/api/cluster/v1alpha1"
	_ "open-cluster-management.io/api/cluster/v1beta1"
	_ "open-cluster-management.io/api/work/v1"
)
