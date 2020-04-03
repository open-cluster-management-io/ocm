package bootstrap

import (
	"io/ioutil"

	"github.com/spf13/pflag"
)

const (
	defaultComponentNamespace = "open-cluster-management"
)

// Options holds the arguments for bootstrapping
type Options struct {
	ClusterNameOverride       string
	HubKubeconfigSecret       string
	BootstrapKubeconfigSecret string
	CertStoreSecret           string
}

// NewOptions return a bootstrap Options with default value
func NewOptions() *Options {
	return &Options{
		HubKubeconfigSecret:       "hub-kubeconfig-secret",
		BootstrapKubeconfigSecret: "default/bootstrap-kubeconfig-secret",
		CertStoreSecret:           "cert-store-secret",
	}
}

// AddFlags registers flags for bootstrap
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClusterNameOverride, "cluster-name-override", o.ClusterNameOverride, "If non-empty, will use this string as cluster name instead of generated random name.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub connection.")
	fs.StringVar(&o.BootstrapKubeconfigSecret, "bootstrap-kubeconfig-secret", o.BootstrapKubeconfigSecret,
		"The name of secret containing kubeconfig for spoke agent bootstrap in the format of namespace/name.")
	fs.StringVar(&o.CertStoreSecret, "cert-store-secret", o.CertStoreSecret,
		"The name of secret in component namespace storing keys/client certificates against hub.")
}

// Validate verifies the inputs.
func (o *Options) Validate() error {
	return nil
}

// Complete fills in missing values.
func (o *Options) Complete() error {
	componentNamespace := getComponentNamespace()
	o.CertStoreSecret = componentNamespace + "/" + o.CertStoreSecret
	o.HubKubeconfigSecret = componentNamespace + "/" + o.HubKubeconfigSecret
	return nil
}

func getComponentNamespace() string {
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultComponentNamespace
	}
	return string(nsBytes)
}
