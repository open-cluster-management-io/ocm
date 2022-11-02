package admission

import (
	"os"

	admissionserver "github.com/openshift/generic-admission-server/pkg/cmd/server"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	genericapiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clusterwebhook "open-cluster-management.io/registration/pkg/webhook/cluster"
	clustersetbindingwebhook "open-cluster-management.io/registration/pkg/webhook/clustersetbinding"
)

func NewAdmissionHook() *cobra.Command {
	ops := NewOptions()
	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Start Managed Cluster Admission Server",
		RunE: func(c *cobra.Command, args []string) error {
			stopCh := genericapiserver.SetupSignalHandler()

			if err := ops.ServerOptions.Complete(); err != nil {
				return err
			}
			if err := ops.ServerOptions.Validate(args); err != nil {
				return err
			}
			if err := ops.RunAdmissionServer(ops.ServerOptions, stopCh); err != nil {
				return err
			}
			return nil
		},
	}

	flags := cmd.Flags()
	ops.AddFlags(flags)
	return cmd
}

// Config contains the server (the webhook) cert and key.
type Options struct {
	QPS           float32
	Burst         int
	ServerOptions *admissionserver.AdmissionServerOptions
}

// NewOptions constructs a new set of default options for webhook.
func NewOptions() *Options {
	return &Options{
		QPS:   100.0,
		Burst: 200,
		ServerOptions: admissionserver.NewAdmissionServerOptions(
			os.Stdout,
			os.Stderr,
			&clusterwebhook.ManagedClusterValidatingAdmissionHook{},
			&clusterwebhook.ManagedClusterMutatingAdmissionHook{},
			&clustersetbindingwebhook.ManagedClusterSetBindingValidatingAdmissionHook{}),
	}
}

func (c *Options) AddFlags(fs *pflag.FlagSet) {
	fs.Float32Var(&c.QPS, "max-qps", c.QPS,
		"Maximum QPS to the hub server from this webhook.")
	fs.IntVar(&c.Burst, "max-burst", c.Burst,
		"Maximum burst for throttle.")

	featureGate := utilfeature.DefaultMutableFeatureGate
	featureGate.AddFlag(fs)

	c.ServerOptions.RecommendedOptions.FeatureGate = featureGate
	c.ServerOptions.RecommendedOptions.AddFlags(fs)
}

// change the default QPS and Butst, so rewrite this func
func (c *Options) RunAdmissionServer(o *admissionserver.AdmissionServerOptions, stopCh <-chan struct{}) error {
	config, err := o.Config()
	if err != nil {
		return err
	}
	config.RestConfig.QPS = c.QPS
	config.RestConfig.Burst = c.Burst
	server, err := config.Complete().New()
	if err != nil {
		return err
	}
	return server.GenericAPIServer.PrepareRun().Run(stopCh)
}
