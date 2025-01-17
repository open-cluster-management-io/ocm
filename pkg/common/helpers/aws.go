package helpers

import "strings"

// GetAwsAccountIdAndClusterName Parses aws accountId and cluster-name from clusterArn
// e.g. if clusterArn is arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1
// accountId is 123456789012 and clusterName is hub-cluster1
func GetAwsAccountIdAndClusterName(clusterArn string) (string, string) {
	clusterStringParts := strings.Split(clusterArn, ":")
	clusterName := strings.Split(clusterStringParts[5], "/")[1]
	awsAccountId := clusterStringParts[4]
	return awsAccountId, clusterName
}

// GetAwsRegion Parses aws accountId and cluster-name from clusterArn
// e.g. if clusterArn is arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1
// awsRegion is us-west-2
func GetAwsRegion(clusterArn string) string {
	clusterStringParts := strings.Split(clusterArn, ":")
	return clusterStringParts[3]
}
