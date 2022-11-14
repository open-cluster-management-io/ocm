package apiserver

import (
	"context"
	"fmt"
	"net"

	"github.com/spf13/pflag"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/kubernetes/cmd/kube-apiserver/app/options"
	netutils "k8s.io/utils/net"

	"open-cluster-management.io/ocm-controlplane/pkg/apiserver/kubeapiserver"
	"open-cluster-management.io/ocm-controlplane/pkg/etcd"
)

type Options struct {
	ServerRunOptions *options.ServerRunOptions
	EmbeddedEtcd     *EmbeddedEtcd
	ClientKeyFile    string
}

type completedOptions struct {
	ServerRunOptions *kubeapiserver.CompletedServerRunOptions
	EmbeddedEtcd     *EmbeddedEtcd
	ClientKeyFile    string
}

type CompletedOptions struct {
	*completedOptions
}

func NewServerRunOptions() *Options {
	o := options.NewServerRunOptions()

	s := Options{
		ServerRunOptions: o,
		EmbeddedEtcd:     NewEmbeddedEtcd(),
	}
	return &s
}

func (o *Options) Validate(args []string) error {
	errors := []error{}
	errors = append(errors, o.ServerRunOptions.Validate()...)
	errors = append(errors, o.EmbeddedEtcd.Validate()...)
	return utilerrors.NewAggregate(errors)
}

func (e *Options) AddFlags(fs *pflag.FlagSet) {
	e.ServerRunOptions.SecureServing.AddFlags(fs)
	e.ServerRunOptions.Etcd.AddFlags(fs)
	e.ServerRunOptions.Authentication.AddFlags(fs)
	e.ServerRunOptions.Authorization.AddFlags(fs)
	e.ServerRunOptions.Admission.AddFlags(fs)
	e.ServerRunOptions.GenericServerRunOptions.AddUniversalFlags(fs)
	e.EmbeddedEtcd.AddFlags(fs)

	fs.StringVar(&e.ClientKeyFile, "client-key-file", e.ClientKeyFile, "client cert key file")
	fs.StringVar(&e.ServerRunOptions.ServiceAccountSigningKeyFile, "service-account-signing-key-file", e.ServerRunOptions.ServiceAccountSigningKeyFile, ""+
		"Path to the file that contains the current private key of the service account token issuer. The issuer will sign issued ID tokens with this private key.")
	fs.StringVar(&e.ServerRunOptions.ServiceClusterIPRanges, "service-cluster-ip-range", e.ServerRunOptions.ServiceClusterIPRanges, ""+
		"A CIDR notation IP range from which to assign service cluster IPs. This must not "+
		"overlap with any IP ranges assigned to nodes or pods. Max of two dual-stack CIDRs is allowed.")
}

func (o *Options) Complete() (*CompletedOptions, error) {
	s, err := kubeapiserver.Complete(o.ServerRunOptions)
	if err != nil {
		return nil, err
	}

	c := completedOptions{
		ServerRunOptions: &s,
		EmbeddedEtcd:     o.EmbeddedEtcd,
		ClientKeyFile:    o.ClientKeyFile,
	}

	return &CompletedOptions{&c}, nil
}

func (c *CompletedOptions) Run() error {
	// set etcd to embeddedetcd info
	if c.EmbeddedEtcd != nil && c.EmbeddedEtcd.Enabled {
		es := &etcd.Server{
			Dir: c.EmbeddedEtcd.Directory,
		}
		embeddedClientInfo, err := es.Run(context.Background(), c.EmbeddedEtcd.PeerPort, c.EmbeddedEtcd.ClientPort, c.EmbeddedEtcd.WalSizeBytes)
		if err != nil {
			return err
		}

		c.ServerRunOptions.Etcd.StorageConfig.Transport.ServerList = embeddedClientInfo.Endpoints
		c.ServerRunOptions.Etcd.StorageConfig.Transport.KeyFile = embeddedClientInfo.KeyFile
		c.ServerRunOptions.Etcd.StorageConfig.Transport.CertFile = embeddedClientInfo.CertFile
		c.ServerRunOptions.Etcd.StorageConfig.Transport.TrustedCAFile = embeddedClientInfo.TrustedCAFile
	}

	// to generate self-signed certificates
	if err := c.ServerRunOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	return kubeapiserver.Run(*c.ServerRunOptions, c.ClientKeyFile, genericapiserver.SetupSignalHandler())
}
