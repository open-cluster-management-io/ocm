package factory

import (
	"testing"

	awsirsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		opt       *Options
		expectErr bool
	}{
		{
			name: "csr validate",
			opt: &Options{
				RegistrationAuth: "csr",
				CSROption: &csr.Option{
					ExpirationSeconds: 1200,
				},
			},
			expectErr: true,
		},
		{
			name: "csr validate pass",
			opt: &Options{
				RegistrationAuth: "csr",
				CSROption: &csr.Option{
					ExpirationSeconds: 7200,
				},
			},
			expectErr: false,
		},
		{
			name: "aws validate",
			opt: &Options{
				RegistrationAuth: "awsirsa",
				AWSISRAOption:    &awsirsa.AWSOption{},
			},
			expectErr: true,
		},
		{
			name: "aws validate pass",
			opt: &Options{
				RegistrationAuth: "awsirsa",
				AWSISRAOption: &awsirsa.AWSOption{
					HubClusterArn: "arn:aws:iam::123456789012:role/aws-iam-authenticator",
				},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opt.Validate()
			if tt.expectErr && err == nil {
				t.Errorf("expect error but got nil")
			}
		})
	}
}
