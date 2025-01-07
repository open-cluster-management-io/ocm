package helpers

import "strings"

func GetAwsAccountIdAndClusterName(clusterArn string) (string, string) {
	clusterStringParts := strings.Split(clusterArn, ":")
	clusterName := strings.Split(clusterStringParts[5], "/")[1]
	awsAccountId := clusterStringParts[4]
	return awsAccountId, clusterName
}

func GetAwsRegion(clusterArn string) string {
	clusterStringParts := strings.Split(clusterArn, ":")
	return clusterStringParts[3]
}
