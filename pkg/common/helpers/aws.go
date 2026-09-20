package helpers

import (
	"crypto/md5" // #nosec G501
	"encoding/hex"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
)

// DefaultAwsPartition is the AWS commercial partition. It is used as a fallback when a
// cluster ARN carries no parsable partition.
const DefaultAwsPartition = "aws"

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

// GetAwsPartition parses the aws partition from clusterArn.
// e.g. if clusterArn is arn:aws-us-gov:eks:us-gov-west-1:123456789012:cluster/hub-cluster1
// the partition is aws-us-gov.
// Every ARN the operator derives from a cluster ARN has to stay in that same partition,
// otherwise IAM and EKS calls fail outside the commercial partition. When clusterArn does
// not parse, fall back to the commercial partition so existing deployments are unaffected.
func GetAwsPartition(clusterArn string) string {
	parsedArn, err := arn.Parse(clusterArn)
	if err != nil || parsedArn.Partition == "" {
		return DefaultAwsPartition
	}
	return parsedArn.Partition
}

// BuildIamRoleArn builds an IAM role ARN in the given partition.
// IAM is a global service, so the region section of the ARN is always empty.
func BuildIamRoleArn(partition string, awsAccountId string, roleName string) string {
	return arn.ARN{
		Partition: partition,
		Service:   "iam",
		AccountID: awsAccountId,
		Resource:  "role/" + roleName,
	}.String()
}

func Md5HashSuffix(hubClusterAccountId string, hubClusterName string, managedClusterAccountId string, managedClusterName string) string {
	hash := md5.Sum([]byte(strings.Join([]string{hubClusterAccountId, hubClusterName, managedClusterAccountId, managedClusterName}, "#"))) // #nosec G401
	return hex.EncodeToString(hash[:])
}
