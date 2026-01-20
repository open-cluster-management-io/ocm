package token

import (
	"errors"

	"github.com/spf13/pflag"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// Option contains configuration for the token driver
type Option struct {
	// ExpirationSeconds is the requested duration of validity of the token.
	// This is used to configure the ServiceAccount token projection.
	// Default is 31536000 seconds (1 year)
	ExpirationSeconds int64
}

// Ensure Option implements register.TokenConfiguration interface at compile time
var _ register.TokenConfiguration = &Option{}

func NewTokenOption() *Option {
	return &Option{
		ExpirationSeconds: 31536000, // Default 1 year
	}
}

func (o *Option) AddFlags(fs *pflag.FlagSet) {
	fs.Int64Var(&o.ExpirationSeconds, "addon-token-expiration-seconds", o.ExpirationSeconds,
		"Requested duration of validity of the token in seconds. Used for ServiceAccount token projection. Minimum 600 seconds (10 minutes)")
}

func (o *Option) Validate() error {
	if o.ExpirationSeconds < 600 {
		return errors.New("token expiration seconds must be at least 600 (10 minutes)")
	}
	return nil
}

func (o *Option) GetExpirationSeconds() int64 {
	return o.ExpirationSeconds
}
