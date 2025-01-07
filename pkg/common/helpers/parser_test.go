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
