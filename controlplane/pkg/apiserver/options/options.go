/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package options

import (
	"errors"
	"net"
	"time"

	"github.com/spf13/pflag"
	apiextensionsapiserver "k8s.io/apiextensions-apiserver/pkg/apiserver"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics"
	aggregatorscheme "k8s.io/kube-aggregator/pkg/apiserver/scheme"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/controlplane/reconcilers"
	kubeletclient "k8s.io/kubernetes/pkg/kubelet/client"
	"k8s.io/kubernetes/pkg/serviceaccount"
	netutils "k8s.io/utils/net"
)

// DefaultServiceNodePortRange is the default port range for NodePort services.
var DefaultServiceNodePortRange = utilnet.PortRange{Base: 30000, Size: 2768}

// DefaultServiceIPCIDR is a CIDR notation of IP range from which to allocate service cluster IPs
var DefaultServiceIPCIDR = net.IPNet{IP: netutils.ParseIPSloppy("10.0.0.0"), Mask: net.CIDRMask(24, 32)}

// DefaultEtcdPathPrefix is the default key prefix of etcd for API Server
const DefaultEtcdPathPrefix = "/registry"

// ServerRunOptions runs a kubernetes api server.
type ServerRunOptions struct {
	GenericServerRunOptions *genericoptions.ServerRunOptions
	Etcd                    *genericoptions.EtcdOptions
	SecureServing           *genericoptions.SecureServingOptionsWithLoopback
	Audit                   *genericoptions.AuditOptions
	Features                *genericoptions.FeatureOptions
	Admission               *AdmissionOptions
	Authentication          *BuiltInAuthenticationOptions
	Authorization           *BuiltInAuthorizationOptions
	APIEnablement           *genericoptions.APIEnablementOptions
	EgressSelector          *genericoptions.EgressSelectorOptions
	Metrics                 *metrics.Options
	Logs                    *logs.Options
	Traces                  *genericoptions.TracingOptions

	AllowPrivileged          bool
	EventTTL                 time.Duration
	MaxConnectionBytesPerSec int64
	// ServiceClusterIPRange is mapped to input provided by user
	ServiceClusterIPRanges string
	// APIServerServiceIP is the first valid IP from PrimaryServiceClusterIPRange
	APIServerServiceIP net.IP
	// PrimaryServiceClusterIPRange and SecondaryServiceClusterIPRange are the results
	// of parsing ServiceClusterIPRange into actual values
	PrimaryServiceClusterIPRange   net.IPNet
	SecondaryServiceClusterIPRange net.IPNet

	EnableAggregatorRouting bool

	EndpointReconcilerType string

	IdentityLeaseDurationSeconds      int
	IdentityLeaseRenewIntervalSeconds int

	ServiceAccountSigningKeyFile     string
	ServiceAccountIssuer             serviceaccount.TokenGenerator
	ServiceAccountTokenMaxExpiration time.Duration

	KubeletConfig kubeletclient.KubeletClientConfig

	ShowHiddenMetricsForVersion string

	EmbeddedEtcd  *EmbeddedEtcd
	ClientKeyFile string
}

// NewServerRunOptions creates a new ServerRunOptions object with default parameters
func NewServerRunOptions() *ServerRunOptions {
	s := ServerRunOptions{
		GenericServerRunOptions: genericoptions.NewServerRunOptions(),
		Etcd:                    genericoptions.NewEtcdOptions(storagebackend.NewDefaultConfig(DefaultEtcdPathPrefix, nil)),
		SecureServing:           NewSecureServingOptions(),
		Audit:                   genericoptions.NewAuditOptions(),
		Features:                genericoptions.NewFeatureOptions(),
		Admission:               NewAdmissionOptions(),
		Authentication:          NewBuiltInAuthenticationOptions().WithAll(),
		Authorization:           NewBuiltInAuthorizationOptions(),
		APIEnablement:           genericoptions.NewAPIEnablementOptions(),
		EgressSelector:          genericoptions.NewEgressSelectorOptions(),
		Metrics:                 metrics.NewOptions(),
		Logs:                    logs.NewOptions(),
		Traces:                  genericoptions.NewTracingOptions(),

		EventTTL:                          1 * time.Hour,
		EndpointReconcilerType:            string(reconcilers.LeaseEndpointReconcilerType),
		IdentityLeaseDurationSeconds:      3600,
		IdentityLeaseRenewIntervalSeconds: 10,

		// this is fake config, just to let server start
		KubeletConfig: kubeletclient.KubeletClientConfig{
			Port:         10250,
			ReadOnlyPort: 10255,
			PreferredAddressTypes: []string{
				"Hostname",
				"InternalDNS",
				"InternalIP",
				"ExternalDNS",
				"ExternalIP",
			},
			HTTPTimeout: time.Duration(5) * time.Second,
		},

		EmbeddedEtcd: NewEmbeddedEtcd(),
	}

	// Overwrite the default for storage data format.
	s.Etcd.DefaultStorageMediaType = "application/vnd.kubernetes.protobuf"

	return &s
}

func (s *ServerRunOptions) Validate(args []string) error {
	errs := []error{}
	errs = append(errs, s.Etcd.Validate()...)
	errs = append(errs, s.SecureServing.Validate()...)
	errs = append(errs, s.Authentication.Validate()...)
	errs = append(errs, s.Authorization.Validate()...)
	errs = append(errs, s.Audit.Validate()...)
	errs = append(errs, s.Admission.Validate()...)
	errs = append(errs, s.APIEnablement.Validate(legacyscheme.Scheme, apiextensionsapiserver.Scheme, aggregatorscheme.Scheme)...)
	errs = append(errs, validateTokenRequest(s)...)
	errs = append(errs, s.Metrics.Validate()...)
	errs = append(errs, s.EmbeddedEtcd.Validate()...)
	return utilerrors.NewAggregate(errs)
}

func validateTokenRequest(options *ServerRunOptions) []error {
	var errs []error

	enableAttempted := options.ServiceAccountSigningKeyFile != "" ||
		(len(options.Authentication.ServiceAccounts.Issuers) != 0 && options.Authentication.ServiceAccounts.Issuers[0] != "") ||
		len(options.Authentication.APIAudiences) != 0

	enableSucceeded := options.ServiceAccountIssuer != nil

	if !enableAttempted {
		errs = append(errs, errors.New("--service-account-signing-key-file and --service-account-issuer are required flags"))
	}

	if enableAttempted && !enableSucceeded {
		errs = append(errs, errors.New("--service-account-signing-key-file, --service-account-issuer, and --api-audiences should be specified together"))
	}

	return errs
}

func (e *ServerRunOptions) AddFlags(fs *pflag.FlagSet) {
	e.SecureServing.AddFlags(fs)
	e.Etcd.AddFlags(fs)
	e.Authentication.AddFlags(fs)
	e.Authorization.AddFlags(fs)
	e.Admission.AddFlags(fs)
	e.GenericServerRunOptions.AddUniversalFlags(fs)
	e.EmbeddedEtcd.AddFlags(fs)

	fs.StringVar(&e.ClientKeyFile, "client-key-file", e.ClientKeyFile, "client cert key file")
	fs.StringVar(&e.ServiceAccountSigningKeyFile, "service-account-signing-key-file", e.ServiceAccountSigningKeyFile, ""+
		"Path to the file that contains the current private key of the service account token issuer. The issuer will sign issued ID tokens with this private key.")
	fs.StringVar(&e.ServiceClusterIPRanges, "service-cluster-ip-range", e.ServiceClusterIPRanges, ""+
		"A CIDR notation IP range from which to assign service cluster IPs. This must not "+
		"overlap with any IP ranges assigned to nodes or pods. Max of two dual-stack CIDRs is allowed.")
}
