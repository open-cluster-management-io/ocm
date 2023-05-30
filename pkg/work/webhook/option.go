package webhook

import "github.com/spf13/pflag"

// Config contains the server (the webhook) cert and key.
type Options struct {
	Port          int
	CertDir       string
	ManifestLimit int
}

// NewOptions constructs a new set of default options for webhook.
func NewOptions() *Options {
	return &Options{
		Port:          9443,
		ManifestLimit: 500 * 1024, // the default manifest limit is 500k.
	}
}

func (c *Options) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.Port, "port", c.Port,
		"Port is the port that the webhook server serves at.")
	fs.StringVar(&c.CertDir, "certdir", c.CertDir,
		"CertDir is the directory that contains the server key and certificate. If not set, webhook server would look up the server key and certificate in {TempDir}/k8s-webhook-server/serving-certs")
	fs.IntVar(&c.ManifestLimit, "manifestLimit", c.ManifestLimit,
		"ManifestLimit is the max size of manifests in a manifestWork. If not set, the default is 500k.")
}
