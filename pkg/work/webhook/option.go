package webhook

import "github.com/spf13/pflag"

// Config contains the server (the webhook) cert and key.
type Options struct {
	ManifestLimit int
}

// NewOptions constructs a new set of default options for webhook.
func NewOptions() *Options {
	return &Options{
		ManifestLimit: 500 * 1024, // the default manifest limit is 500k.
	}
}

func (c *Options) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.ManifestLimit, "manifestLimit", c.ManifestLimit,
		"ManifestLimit is the max size of manifests in a manifestWork. If not set, the default is 500k.")
}
