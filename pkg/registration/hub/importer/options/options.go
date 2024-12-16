package options

import "github.com/spf13/pflag"

type Options struct {
	APIServerURL string
}

func New() *Options {
	return &Options{}
}

// AddFlags registers flags for manager
func (m *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&m.APIServerURL, "hub-apiserver-url", m.APIServerURL,
		"APIServer URL of the hub cluster that the spoke cluster can access, Only used for spoke cluster import")
}
