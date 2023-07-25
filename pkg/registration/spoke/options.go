package spoke

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"open-cluster-management.io/ocm/pkg/registration/helpers"
)

// SpokeAgentOptions holds configuration for spoke cluster agent
type SpokeAgentOptions struct {
	BootstrapKubeconfig         string
	HubKubeconfigSecret         string
	SpokeExternalServerURLs     []string
	ClusterHealthCheckPeriod    time.Duration
	MaxCustomClusterClaims      int
	ClientCertExpirationSeconds int32
	ClusterAnnotations          map[string]string
}

func NewSpokeAgentOptions() *SpokeAgentOptions {
	return &SpokeAgentOptions{
		HubKubeconfigSecret:      "hub-kubeconfig-secret",
		ClusterHealthCheckPeriod: 1 * time.Minute,
		MaxCustomClusterClaims:   20,
	}
}

// AddFlags registers flags for Agent
func (o *SpokeAgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.BootstrapKubeconfig, "bootstrap-kubeconfig", o.BootstrapKubeconfig,
		"The path of the kubeconfig file for agent bootstrap.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringArrayVar(&o.SpokeExternalServerURLs, "spoke-external-server-urls", o.SpokeExternalServerURLs,
		"A list of reachable spoke cluster api server URLs for hub cluster.")
	fs.DurationVar(&o.ClusterHealthCheckPeriod, "cluster-healthcheck-period", o.ClusterHealthCheckPeriod,
		"The period to check managed cluster kube-apiserver health")
	fs.IntVar(&o.MaxCustomClusterClaims, "max-custom-cluster-claims", o.MaxCustomClusterClaims,
		"The max number of custom cluster claims to expose.")
	fs.Int32Var(&o.ClientCertExpirationSeconds, "client-cert-expiration-seconds", o.ClientCertExpirationSeconds,
		"The requested duration in seconds of validity of the issued client certificate. If this is not set, "+
			"the value of --cluster-signing-duration command-line flag of the kube-controller-manager will be used.")
	fs.StringToStringVar(&o.ClusterAnnotations, "cluster-annotations", o.ClusterAnnotations, `the annotations with the reserve
	 prefix "agent.open-cluster-management.io" set on ManagedCluster when creating only, other actors can update it afterwards.`)
}

// Validate verifies the inputs.
func (o *SpokeAgentOptions) Validate() error {
	if o.BootstrapKubeconfig == "" {
		return errors.New("bootstrap-kubeconfig is required")
	}

	// if SpokeExternalServerURLs is specified we validate every URL in it, we expect the spoke external server URL is https
	if len(o.SpokeExternalServerURLs) != 0 {
		for _, serverURL := range o.SpokeExternalServerURLs {
			if !helpers.IsValidHTTPSURL(serverURL) {
				return fmt.Errorf("%q is invalid", serverURL)
			}
		}
	}

	if o.ClusterHealthCheckPeriod <= 0 {
		return errors.New("cluster healthcheck period must greater than zero")
	}

	if o.ClientCertExpirationSeconds != 0 && o.ClientCertExpirationSeconds < 3600 {
		return errors.New("client certificate expiration seconds must greater or qual to 3600")
	}

	return nil
}
