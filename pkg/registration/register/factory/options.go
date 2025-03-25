package factory

import (
	"github.com/spf13/pflag"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
	awsirsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

type Options struct {
	RegistrationAuth string
	CSROption        *csr.Option
	AWSISRAOption    *awsirsa.AWSOption
}

func NewOptions() *Options {
	return &Options{
		CSROption:     csr.NewCSROption(),
		AWSISRAOption: awsirsa.NewAWSOption(),
	}
}

func (s *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.RegistrationAuth, "registration-auth", s.RegistrationAuth,
		"The type of authentication to use to authenticate with hub.")
	s.CSROption.AddFlags(fs)
	s.AWSISRAOption.AddFlags(fs)
}

func (s *Options) Validate() error {
	switch s.RegistrationAuth {
	case helpers.AwsIrsaAuthType:
		return s.AWSISRAOption.Validate()
	default:
		return s.CSROption.Validate()
	}
}

func (s *Options) Driver(secretOption register.SecretOption) register.RegisterDriver {
	switch s.RegistrationAuth {
	case helpers.AwsIrsaAuthType:
		return awsirsa.NewAWSIRSADriver(s.AWSISRAOption, secretOption)
	default:
		return csr.NewCSRDriver(s.CSROption, secretOption)
	}
}
