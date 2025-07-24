package grpc

import (
	"fmt"

	"github.com/spf13/pflag"
)

type Option struct {
	BootstrapConfigFile string
	ConfigFile          string
}

func NewOptions() *Option {
	return &Option{}
}

func (o *Option) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.BootstrapConfigFile, "grpc-bootstrap-config", o.BootstrapConfigFile, "")
	fs.StringVar(&o.ConfigFile, "grpc-config", o.ConfigFile, "")
}

func (o *Option) Validate() error {
	if o.ConfigFile == "" && o.BootstrapConfigFile == "" {
		return fmt.Errorf("config file should be set")
	}

	return nil
}
