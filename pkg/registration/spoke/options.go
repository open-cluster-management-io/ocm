package spoke

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
)

// SpokeAgentOptions holds configuration for spoke cluster agent
type SpokeAgentOptions struct {
	// The differences among BootstrapKubeconfig, BootstrapKubeconfigSecret, BootstrapKubeconfigSecrets are:
	// 1. BootstrapKubeconfig is a file path, the controller uses it to build the client.
	// 2. BootstrapKubeconfigSecret is the secret, an event handler will watch it, if the secret is changed, then rebootstrap.
	// 3. BootstrapKubeconfigs is a list of file path, the controller uses one of its item to build the client.
	// BootstrapKubeconfigs can only be used when MultipleHubs is enabled.
	BootstrapKubeconfig       string
	BootstrapKubeconfigSecret string
	BootstrapKubeconfigs      []string

	// TODO: The hubConnectionTimoutSeconds should always greater than leaseDurationSeconds, we need to make timeout as a build-in part of
	// leaseController in the future and relate timeoutseconds to leaseDurationSeconds. @xuezhaojun
	// See more details in: https://github.com/open-cluster-management-io/ocm/pull/443#discussion_r1610868646
	HubConnectionTimeoutSeconds int32

	HubKubeconfigSecret          string
	SpokeExternalServerURLs      []string
	ClusterHealthCheckPeriod     time.Duration
	MaxCustomClusterClaims       int
	ReservedClusterClaimSuffixes []string
	ClusterAnnotations           map[string]string

	RegisterDriverOption *registerfactory.Options
}

func NewSpokeAgentOptions() *SpokeAgentOptions {
	options := &SpokeAgentOptions{
		BootstrapKubeconfigSecret:   "bootstrap-hub-kubeconfig",
		HubKubeconfigSecret:         "hub-kubeconfig-secret",
		ClusterHealthCheckPeriod:    1 * time.Minute,
		MaxCustomClusterClaims:      20,
		HubConnectionTimeoutSeconds: 600, // by default, the timeout is 10 minutes

		RegisterDriverOption: registerfactory.NewOptions(),
	}

	return options
}

// AddFlags registers flags for Agent
func (o *SpokeAgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.BootstrapKubeconfig, "bootstrap-kubeconfig", o.BootstrapKubeconfig,
		"The path of the kubeconfig file for agent bootstrap.")
	fs.StringVar(&o.BootstrapKubeconfigSecret, "bootstrap-kubeconfig-secret", o.BootstrapKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for agent bootstrap.")
	fs.StringArrayVar(&o.BootstrapKubeconfigs, "bootstrap-kubeconfigs", o.BootstrapKubeconfigs,
		"The name of secrets in component namespace storing bootstrap kubeconfigs for agent bootstrap.")
	fs.Int32Var(&o.HubConnectionTimeoutSeconds, "hub-connection-timeout-seconds", o.HubConnectionTimeoutSeconds,
		"The timeout in seconds to connect to hub cluster.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringArrayVar(&o.SpokeExternalServerURLs, "spoke-external-server-urls", o.SpokeExternalServerURLs,
		"A list of reachable spoke cluster api server URLs for hub cluster.")
	fs.DurationVar(&o.ClusterHealthCheckPeriod, "cluster-healthcheck-period", o.ClusterHealthCheckPeriod,
		"The period to check managed cluster kube-apiserver health")
	fs.IntVar(&o.MaxCustomClusterClaims, "max-custom-cluster-claims", o.MaxCustomClusterClaims,
		"The max number of custom cluster claims to expose.")
	fs.StringSliceVar(&o.ReservedClusterClaimSuffixes, "reserved-cluster-claim-suffixes", o.ReservedClusterClaimSuffixes,
		"A list of suffixes for reserved cluster claims.")
	fs.StringToStringVar(&o.ClusterAnnotations, "cluster-annotations", o.ClusterAnnotations, `the annotations with the reserve
	 prefix "agent.open-cluster-management.io" set on ManagedCluster when creating only, other actors can update it afterwards.`)

	o.RegisterDriverOption.AddFlags(fs)
}

// Validate verifies the inputs.
func (o *SpokeAgentOptions) Validate() error {
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.MultipleHubs) {
		// expect BootstrapKubeconfig is empty and BootstrapKubeconfigs has at least 2 items
		if len(o.BootstrapKubeconfigs) < 2 {
			return errors.New("expect at least 2 bootstrap kubeconfigs")
		}
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

	if err := o.RegisterDriverOption.Validate(); err != nil {
		return err
	}

	return nil
}
