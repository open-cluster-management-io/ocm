package factory

import (
	"github.com/spf13/pflag"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
	awsirsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/grpc"
)

type Options struct {
	RegistrationAuth string
	CSROption        *csr.Option
	AWSIRSAOption    *awsirsa.AWSOption
	GRPCOption       *grpc.Option
}

func NewOptions() *Options {
	return &Options{
		CSROption:     csr.NewCSROption(),
		AWSIRSAOption: awsirsa.NewAWSOption(),
		GRPCOption:    grpc.NewOptions(),
	}
}

func (s *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.RegistrationAuth, "registration-auth", s.RegistrationAuth,
		"The type of authentication to use to authenticate with hub.")
	s.CSROption.AddFlags(fs)
	s.AWSIRSAOption.AddFlags(fs)
	s.GRPCOption.AddFlags(fs)
}

func (s *Options) Validate() error {
	switch s.RegistrationAuth {
	case operatorv1.AwsIrsaAuthType:
		return s.AWSIRSAOption.Validate()
	case operatorv1.GRPCAuthType:
		return s.GRPCOption.Validate()
	default:
		return s.CSROption.Validate()
	}
}

func (s *Options) Driver(secretOption register.SecretOption) (register.RegisterDriver, error) {
	switch s.RegistrationAuth {
	case operatorv1.AwsIrsaAuthType:
		return awsirsa.NewAWSIRSADriver(s.AWSIRSAOption, secretOption), nil
	case operatorv1.GRPCAuthType:
		return grpc.NewGRPCDriver(s.GRPCOption, s.CSROption, secretOption)
	default:
		return csr.NewCSRDriver(s.CSROption, secretOption)
	}
}
