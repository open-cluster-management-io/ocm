package user

const (
	// SubjectPrefix is a prefix for marking open-cluster-management users
	SubjectPrefix = "system:open-cluster-management:"
	// SpokeClustersGroup is a common group for all spoke clusters
	SpokeClustersGroup = SubjectPrefix + "spoke-clusters"
)
