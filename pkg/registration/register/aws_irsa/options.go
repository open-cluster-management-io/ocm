package aws_irsa

import (
	"errors"

	"github.com/spf13/pflag"
)

// AWSOption includes options that is used to monitor ManagedClusters
type AWSOption struct {
	HubClusterArn            string
	ManagedClusterArn        string
	ManagedClusterRoleSuffix string
}

func NewAWSOption() *AWSOption {
	return &AWSOption{}
}

func (o *AWSOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.HubClusterArn, "hub-cluster-arn", o.HubClusterArn,
		"The ARN of the EKS based hub cluster.")
	fs.StringVar(&o.ManagedClusterArn, "managed-cluster-arn", o.ManagedClusterArn,
		"The ARN of the EKS based managed cluster.")
	fs.StringVar(&o.ManagedClusterRoleSuffix, "managed-cluster-role-suffix", o.ManagedClusterRoleSuffix,
		"The suffix of the managed cluster IAM role.")
}

func (o *AWSOption) Validate() error {
	if o.HubClusterArn == "" {
		return errors.New("EksHubClusterArn cannot be empty if RegistrationAuth is awsirsa")
	}
	return nil
}
