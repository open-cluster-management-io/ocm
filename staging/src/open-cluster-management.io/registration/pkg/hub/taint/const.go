package taint

import (
	v1 "open-cluster-management.io/api/cluster/v1"
)

var (
	UnavailableTaint = v1.Taint{
		Key:    v1.ManagedClusterTaintUnavailable,
		Effect: v1.TaintEffectNoSelect,
	}

	UnreachableTaint = v1.Taint{
		Key:    v1.ManagedClusterTaintUnreachable,
		Effect: v1.TaintEffectNoSelect,
	}
)
