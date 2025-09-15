package factory

import (
	"testing"

	"github.com/spf13/pflag"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
	awsirsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/grpc"
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
				AWSIRSAOption:    &awsirsa.AWSOption{},
			},
			expectErr: true,
		},
		{
			name: "aws validate pass",
			opt: &Options{
				RegistrationAuth: "awsirsa",
				AWSIRSAOption: &awsirsa.AWSOption{
					HubClusterArn: "arn:aws:iam::123456789012:role/aws-iam-authenticator",
				},
			},
			expectErr: false,
		},
		{
			name: "grpc validate",
			opt: &Options{
				RegistrationAuth: "grpc",
				GRPCOption:       &grpc.Option{},
			},
			expectErr: true,
		},
		{
			name: "grpc validate pass (bootstrap config)",
			opt: &Options{
				RegistrationAuth: "grpc",
				GRPCOption: &grpc.Option{
					BootstrapConfigFile: "test-bootstrap-config",
				},
			},
			expectErr: false,
		},
		{
			name: "grpc validate pass",
			opt: &Options{
				RegistrationAuth: "grpc",
				GRPCOption: &grpc.Option{
					ConfigFile: "test-config",
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

func TestNewOptions(t *testing.T) {
	opts := NewOptions()

	if opts == nil {
		t.Fatal("NewOptions() returned nil")
	}

	if opts.CSROption == nil {
		t.Error("Expected CSROption to be initialized")
	}

	if opts.AWSIRSAOption == nil {
		t.Error("Expected AWSIRSAOption to be initialized")
	}

	if opts.GRPCOption == nil {
		t.Error("Expected GRPCOption to be initialized")
	}

	if opts.RegistrationAuth != "" {
		t.Errorf("Expected RegistrationAuth to be empty by default, got %q", opts.RegistrationAuth)
	}
}

func TestAddFlags(t *testing.T) {
	opts := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	opts.AddFlags(fs)

	// Test that registration-auth flag is added
	registrationAuthFlag := fs.Lookup("registration-auth")
	if registrationAuthFlag == nil {
		t.Error("Expected registration-auth flag to be added")
	}

	// Test that flag value can be set
	err := fs.Set("registration-auth", "csr")
	if err != nil {
		t.Errorf("Failed to set registration-auth flag: %v", err)
	}

	if opts.RegistrationAuth != "csr" {
		t.Errorf("Expected RegistrationAuth to be 'csr', got %q", opts.RegistrationAuth)
	}
}

func TestDriver(t *testing.T) {
	secretOption := register.SecretOption{}

	tests := []struct {
		name             string
		registrationAuth string
		expectErr        bool
		driverType       string
	}{
		{
			name:             "CSR driver",
			registrationAuth: "",
			expectErr:        true, // Will error due to missing bootstrap-kubeconfig
			driverType:       "CSR",
		},
		{
			name:             "CSR driver explicit",
			registrationAuth: "csr",
			expectErr:        true, // Will error due to missing bootstrap-kubeconfig
			driverType:       "CSR",
		},
		{
			name:             "AWS IRSA driver",
			registrationAuth: operatorv1.AwsIrsaAuthType,
			expectErr:        false,
			driverType:       "AWS",
		},
		{
			name:             "GRPC driver",
			registrationAuth: operatorv1.GRPCAuthType,
			expectErr:        false, // Won't error since we're not validating, just creating driver
			driverType:       "GRPC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := NewOptions()
			opts.RegistrationAuth = tt.registrationAuth

			driver, err := opts.Driver(secretOption)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if driver == nil {
					t.Error("Expected driver to be non-nil")
				}
			}
		})
	}
}

func TestDriverWithValidGRPC(t *testing.T) {
	secretOption := register.SecretOption{}
	opts := NewOptions()
	opts.RegistrationAuth = operatorv1.GRPCAuthType
	opts.GRPCOption.ConfigFile = "test-config"

	driver, err := opts.Driver(secretOption)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if driver == nil {
		t.Error("Expected driver to be non-nil")
	}
}

func TestValidateUnknownAuthType(t *testing.T) {
	opts := &Options{
		RegistrationAuth: "unknown",
		CSROption: &csr.Option{
			ExpirationSeconds: 7200,
		},
	}

	err := opts.Validate()
	if err != nil {
		t.Errorf("Unknown auth type should fall back to CSR validation, got error: %v", err)
	}
}

func TestOptionsStructure(t *testing.T) {
	opts := &Options{
		RegistrationAuth: "test",
		CSROption:        &csr.Option{},
		AWSIRSAOption:    &awsirsa.AWSOption{},
		GRPCOption:       &grpc.Option{},
	}

	if opts.RegistrationAuth != "test" {
		t.Error("RegistrationAuth field not set correctly")
	}
	if opts.CSROption == nil {
		t.Error("CSROption field not set correctly")
	}
	if opts.AWSIRSAOption == nil {
		t.Error("AWSIRSAOption field not set correctly")
	}
	if opts.GRPCOption == nil {
		t.Error("GRPCOption field not set correctly")
	}
}
