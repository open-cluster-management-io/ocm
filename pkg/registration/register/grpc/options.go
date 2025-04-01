package grpc

import (
	"fmt"
	"os"

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

	if o.BootstrapConfigFile != "" {
		if _, err := os.Stat(o.BootstrapConfigFile); err != nil {
			return fmt.Errorf("bootstrap config file %s is not accessible: %v", o.BootstrapConfigFile, err)
		}
	}

	if o.ConfigFile != "" {
		if _, err := os.Stat(o.ConfigFile); err != nil {
			return fmt.Errorf("config file %s is not accessible: %v", o.ConfigFile, err)
		}
	}

	return nil
}
