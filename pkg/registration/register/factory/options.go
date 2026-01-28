package factory

import (
	"fmt"

	"github.com/spf13/pflag"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
	awsirsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/grpc"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
)

type Options struct {
	RegistrationAuth string
	CSROption        *csr.Option
	AWSIRSAOption    *awsirsa.AWSOption
	GRPCOption       *grpc.Option
	TokenOption      *token.Option

	// AddonKubeClientRegistrationAuth specifies the authentication method for addons
	// with registration type KubeClient. Possible values are "csr" (default) and "token".
	AddonKubeClientRegistrationAuth string
}

func NewOptions() *Options {
	return &Options{
		CSROption:                       csr.NewCSROption(),
		AWSIRSAOption:                   awsirsa.NewAWSOption(),
		GRPCOption:                      grpc.NewOptions(),
		TokenOption:                     token.NewTokenOption(),
		AddonKubeClientRegistrationAuth: "csr", // default to csr
	}
}

func (s *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.RegistrationAuth, "registration-auth", s.RegistrationAuth,
		"The type of authentication to use to authenticate with hub.")
	fs.StringVar(&s.AddonKubeClientRegistrationAuth, "addon-kubeclient-registration-auth", s.AddonKubeClientRegistrationAuth,
		"The authentication method for addons with registration type KubeClient. Possible values are 'csr' (default) and 'token'.")
	s.CSROption.AddFlags(fs)
	s.AWSIRSAOption.AddFlags(fs)
	s.GRPCOption.AddFlags(fs)
	s.TokenOption.AddFlags(fs)
}

func (s *Options) Validate() error {
	switch s.AddonKubeClientRegistrationAuth {
	case "", "csr", "token":
		// valid values
	default:
		return fmt.Errorf("unsupported addon-kubeclient-registration-auth: %s", s.AddonKubeClientRegistrationAuth)
	}

	switch s.RegistrationAuth {
	case operatorv1.AwsIrsaAuthType:
		return s.AWSIRSAOption.Validate()
	case operatorv1.GRPCAuthType:
		return s.GRPCOption.Validate()
	default:
		return s.CSROption.Validate()
	}
}

func (s *Options) GetKubeClientAuth() string {
	return s.AddonKubeClientRegistrationAuth
}

func (s *Options) GetCSRConfiguration() register.CSRConfiguration {
	return s.CSROption
}

func (s *Options) GetTokenConfiguration() register.TokenConfiguration {
	return s.TokenOption
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
