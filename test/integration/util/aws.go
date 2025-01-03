package util

import (
	"strings"

	v1 "k8s.io/api/apps/v1"
)

const (
	HubClusterArn            = "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"
	ManagedClusterArn        = "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1"
	ManagedClusterRoleSuffix = "7f8141296c75f2871e3d030f85c35692"
	PrerequisiteSpokeRoleArn = "arn:aws:iam::123456789012:role/ocm-managed-cluster-" + ManagedClusterRoleSuffix
	IrsaAnnotationKey        = "eks.amazonaws.com/role-arn"
)

func AwsCliSpecificVolumesMounted(deployment v1.Deployment) bool {
	isDotAwsMounted := false
	isAwsCliMounted := false
	for _, volumeMount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		if volumeMount.Name == "dot-aws" && volumeMount.MountPath == "/.aws" {
			isDotAwsMounted = true
		} else if volumeMount.Name == "awscli" && volumeMount.MountPath == "/awscli" {
			isAwsCliMounted = true
		}
	}
	return isDotAwsMounted && isAwsCliMounted
}

func AllCommandLineOptionsPresent(deployment v1.Deployment) bool {
	isRegistrationAuthPresent := false
	isManagedClusterArnPresent := false
	isManagedClusterRoleSuffixPresent := false
	for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
		if strings.Contains(arg, "--registration-auth=awsirsa") {
			isRegistrationAuthPresent = true
		}
		if strings.Contains(arg, "--managed-cluster-arn=arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1") {
			isManagedClusterArnPresent = true
		}
		if strings.Contains(arg, "--managed-cluster-role-suffix="+ManagedClusterRoleSuffix) {
			isManagedClusterRoleSuffixPresent = true
		}
	}
	return isRegistrationAuthPresent && isManagedClusterArnPresent && isManagedClusterRoleSuffixPresent
}
