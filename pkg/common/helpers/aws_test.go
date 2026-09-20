package helpers

import (
	"testing"
)

func TestGetAwsAccountIdAndClusterName(t *testing.T) {

	awsAccountId, clusterName := GetAwsAccountIdAndClusterName("arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster")
	if awsAccountId != "123456789012" && clusterName != "hub-cluster" {
		t.Errorf("awsAccountId and cluster id are not valid")
	}

}

func TestGetAwsPartition(t *testing.T) {
	cases := []struct {
		name       string
		clusterArn string
		expected   string
	}{
		{
			name:       "commercial partition",
			clusterArn: "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster",
			expected:   "aws",
		},
		{
			name:       "govcloud partition",
			clusterArn: "arn:aws-us-gov:eks:us-gov-west-1:123456789012:cluster/hub-cluster",
			expected:   "aws-us-gov",
		},
		{
			name:       "iso partition",
			clusterArn: "arn:aws-iso:eks:us-iso-east-1:123456789012:cluster/hub-cluster",
			expected:   "aws-iso",
		},
		{
			name:       "iso-b partition",
			clusterArn: "arn:aws-iso-b:eks:us-isob-east-1:123456789012:cluster/hub-cluster",
			expected:   "aws-iso-b",
		},
		{
			name:       "china partition",
			clusterArn: "arn:aws-cn:eks:cn-north-1:123456789012:cluster/hub-cluster",
			expected:   "aws-cn",
		},
		{
			name:       "empty arn falls back to the commercial partition",
			clusterArn: "",
			expected:   "aws",
		},
		{
			name:       "malformed arn falls back to the commercial partition",
			clusterArn: "not-an-arn",
			expected:   "aws",
		},
		{
			name:       "arn with an empty partition falls back to the commercial partition",
			clusterArn: "arn::eks:us-west-2:123456789012:cluster/hub-cluster",
			expected:   "aws",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if partition := GetAwsPartition(c.clusterArn); partition != c.expected {
				t.Errorf("expected partition %q, but got %q", c.expected, partition)
			}
		})
	}
}

func TestBuildIamRoleArn(t *testing.T) {
	cases := []struct {
		name      string
		partition string
		accountId string
		roleName  string
		expected  string
	}{
		{
			name:      "commercial partition",
			partition: "aws",
			accountId: "123456789012",
			roleName:  "ocm-hub-role-suffix",
			expected:  "arn:aws:iam::123456789012:role/ocm-hub-role-suffix",
		},
		{
			name:      "govcloud partition",
			partition: "aws-us-gov",
			accountId: "123456789012",
			roleName:  "ocm-hub-role-suffix",
			expected:  "arn:aws-us-gov:iam::123456789012:role/ocm-hub-role-suffix",
		},
		{
			name:      "iso partition",
			partition: "aws-iso",
			accountId: "123456789012",
			roleName:  "hub-cluster_managed-cluster-identity-creator",
			expected:  "arn:aws-iso:iam::123456789012:role/hub-cluster_managed-cluster-identity-creator",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if roleArn := BuildIamRoleArn(c.partition, c.accountId, c.roleName); roleArn != c.expected {
				t.Errorf("expected role arn %q, but got %q", c.expected, roleArn)
			}
		})
	}
}
