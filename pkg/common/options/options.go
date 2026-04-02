package options

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/clock"

	tlslib "open-cluster-management.io/sdk-go/pkg/tls"
)

type Options struct {
	CmdConfig       *controllercmd.ControllerCommandConfig
	Burst           int
	QPS             float32
	TLSMinVersion   string
	TLSCipherSuites string
}

// NewOptions returns the flags with default value set
func NewOptions() *Options {
	opts := &Options{
		QPS:   50,
		Burst: 100,
	}
	return opts
}

func (o *Options) NewControllerCommandConfig(
	componentName string, version version.Info, startFunc controllercmd.StartFunc, clock clock.Clock) *controllercmd.ControllerCommandConfig {
	o.CmdConfig = controllercmd.NewControllerCommandConfig(componentName, version, o.StartWithQPS(startFunc), clock)
	return o.CmdConfig
}

func (o *Options) StartWithQPS(startFunc controllercmd.StartFunc) controllercmd.StartFunc {
	return func(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
		controllerContext.KubeConfig.QPS = o.QPS
		controllerContext.KubeConfig.Burst = o.Burst
		return startFunc(ctx, controllerContext)
	}
}

func (o *Options) AddFlags(flags *pflag.FlagSet) {
	flags.Float32Var(&o.QPS, "kube-api-qps", o.QPS, "QPS to use while talking with apiserver on spoke cluster.")
	flags.IntVar(&o.Burst, "kube-api-burst", o.Burst, "Burst to use while talking with apiserver on spoke cluster.")
	flags.StringVar(&o.TLSMinVersion, "tls-min-version", "",
		"Minimum TLS version for the serving endpoint. Possible values: VersionTLS12, VersionTLS13.")
	flags.StringVar(&o.TLSCipherSuites, "tls-cipher-suites", "",
		"Comma-separated list of TLS cipher suites for the serving endpoint. "+
			"If omitted, the default Go cipher suites are used.")
	if o.CmdConfig != nil {
		flags.BoolVar(&o.CmdConfig.DisableLeaderElection, "disable-leader-election", false, "Disable leader election.")
		flags.DurationVar(&o.CmdConfig.LeaseDuration.Duration, "leader-election-lease-duration", 137*time.Second, ""+
			"The duration that non-leader candidates will wait after observing a leadership "+
			"renewal until attempting to acquire leadership of a led but unrenewed leader "+
			"slot. This is effectively the maximum duration that a leader can be stopped "+
			"before it is replaced by another candidate. This is only applicable if leader "+
			"election is enabled.")
		flags.DurationVar(&o.CmdConfig.RenewDeadline.Duration, "leader-election-renew-deadline", 107*time.Second, ""+
			"The interval between attempts by the acting master to renew a leadership slot "+
			"before it stops leading. This must be less than or equal to the lease duration. "+
			"This is only applicable if leader election is enabled.")
		flags.DurationVar(&o.CmdConfig.RetryPeriod.Duration, "leader-election-retry-period", 26*time.Second, ""+
			"The duration the clients should wait between attempting acquisition and renewal "+
			"of a leadership. This is only applicable if leader election is enabled.")
	}
}

// ApplyTLSToCommand installs a PersistentPreRunE hook that writes a minimal
// GenericOperatorConfig YAML to a temp file in /tmp (which is mounted as an
// emptyDir in all hub controller deployments, so writing is safe even with
// readOnlyRootFilesystem: true) and sets library-go's --config flag to point
// at it. This runs before cmd.Run so all library-go boilerplate (signal
// handling, logging, profiling) is fully preserved.
//
// Inside cmd.Run, StartController calls Config() which reads the file; the TLS
// values survive SetRecommendedHTTPServingInfoDefaults because DefaultString only
// sets fields that are empty.
func (o *Options) ApplyTLSToCommand(cmd *cobra.Command) {
	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		// Only inject when at least one TLS flag is set and --config not already provided.
		if (o.TLSMinVersion == "" && o.TLSCipherSuites == "") || cmd.Flags().Changed("config") {
			return nil
		}
		// Validate TLS flags up-front so invalid values are caught early with a
		// clear error rather than being deferred to library-go's YAML parsing.
		if _, err := tlslib.ConfigFromFlags(o.TLSMinVersion, o.TLSCipherSuites); err != nil {
			return fmt.Errorf("invalid TLS flags: %w", err)
		}
		content := "apiVersion: operator.openshift.io/v1alpha1\nkind: GenericOperatorConfig\nservingInfo:\n"
		if o.TLSMinVersion != "" {
			content += "  minTLSVersion: " + o.TLSMinVersion + "\n"
		}
		if o.TLSCipherSuites != "" {
			content += "  cipherSuites:\n"
			for _, c := range strings.Split(o.TLSCipherSuites, ",") {
				content += "  - " + strings.TrimSpace(c) + "\n"
			}
		}
		// Use /tmp explicitly: it is mounted as an emptyDir in all hub controller
		// deployments so the path is writable even with readOnlyRootFilesystem: true.
		f, err := os.CreateTemp("/tmp", "ocm-controller-tls-*.yaml")
		if err != nil {
			return fmt.Errorf("failed to create TLS config file: %w", err)
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			os.Remove(f.Name())
			return fmt.Errorf("failed to write TLS config: %w", err)
		}
		f.Close()
		return cmd.Flags().Set("config", f.Name())
	}
}
