package user

const (
	// SubjectPrefix is a prefix for marking open-cluster-management users
	SubjectPrefix = "system:open-cluster-management:"
	// ManagedClustersGroup is a common group for all spoke clusters
	ManagedClustersGroup = SubjectPrefix + "managed-clusters"
)
