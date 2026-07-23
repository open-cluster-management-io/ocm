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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

// writeTLSServingConfig writes a minimal GenericOperatorConfig YAML to a temp
// file in /tmp and sets library-go's --config flag to point at it.
// /tmp is mounted as an emptyDir in all operator/agent deployments so writing
// is safe even with readOnlyRootFilesystem: true.
// Inside cmd.Run, StartController calls Config() which reads the file; the TLS
// values survive SetRecommendedHTTPServingInfoDefaults because DefaultString
// only sets fields that are empty.
func writeTLSServingConfig(cmd *cobra.Command, minVersion, cipherSuites string) error {
	content := "apiVersion: operator.openshift.io/v1alpha1\nkind: GenericOperatorConfig\nservingInfo:\n"
	if minVersion != "" {
		content += "  minTLSVersion: " + minVersion + "\n"
	}
	if cipherSuites != "" {
		content += "  cipherSuites:\n"
		for _, c := range strings.Split(cipherSuites, ",") {
			content += "  - " + strings.TrimSpace(c) + "\n"
		}
	}
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

// ApplyTLSToCommand installs a PersistentPreRunE hook that reads --tls-min-version
// and --tls-cipher-suites CLI flags and writes a minimal GenericOperatorConfig
// YAML to a temp file, then sets library-go's --config flag to point at it.
// This runs before cmd.Run so all library-go boilerplate is fully preserved.
// Use this for components whose deployments receive TLS flags from their operator.
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
		return writeTLSServingConfig(cmd, o.TLSMinVersion, o.TLSCipherSuites)
	}
}

// operatorNamespace returns the namespace the operator is running in.
// It checks the --namespace flag first, then falls back to the service account
// namespace file. Returns empty string if neither is available (e.g., local dev).
func operatorNamespace(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("namespace"); f != nil && f.Changed {
		return f.Value.String()
	}
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(nsBytes))
}

// applyTLSFromConfigMap reads the ocm-tls-profile ConfigMap using the given
// kube client and namespace, and if found, writes the TLS serving config file.
// Returns nil if the ConfigMap is not found (graceful no-op).
// Extracted for testability.
func applyTLSFromConfigMap(ctx context.Context, kubeClient kubernetes.Interface, namespace string, cmd *cobra.Command) error {
	tlsCfg, err := tlslib.LoadTLSConfigFromConfigMap(ctx, kubeClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to load TLS config from ConfigMap: %w", err)
	}
	if tlsCfg == nil {
		// ConfigMap not found or empty -- skip gracefully, use library-go defaults.
		return nil
	}
	minVersion := tlslib.VersionToString(tlsCfg.MinVersion)
	cipherSuites := ""
	if len(tlsCfg.CipherSuites) > 0 {
		cipherSuites = tlslib.CipherSuitesToString(tlsCfg.CipherSuites)
	}
	return writeTLSServingConfig(cmd, minVersion, cipherSuites)
}

// ApplyTLSFromConfigMapToCommand installs a PersistentPreRunE hook that reads
// the ocm-tls-profile ConfigMap directly (using the in-cluster service account)
// and writes a minimal GenericOperatorConfig YAML so library-go's GenericAPIServer
// (port 8443) uses the correct TLS settings from the cluster TLS profile.
//
// This is intended for operator binaries that do not receive --tls-min-version
// and --tls-cipher-suites flags from their deployment manifests (because nobody
// injects them externally), but do have access to the ocm-tls-profile ConfigMap
// in their namespace at startup.
//
// If the ConfigMap is not found or the in-cluster config is unavailable (e.g.,
// running locally), the hook is a no-op and library-go's defaults apply.
func (o *Options) ApplyTLSFromConfigMapToCommand(cmd *cobra.Command) {
	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		// Skip if --config already set by the user.
		if cmd.Flags().Changed("config") {
			return nil
		}
		// Get the operator namespace.
		namespace := operatorNamespace(cmd)
		if namespace == "" {
			// Not running in cluster (local dev) -- skip gracefully.
			return nil
		}
		// Build an in-cluster kube client.
		cfg, err := rest.InClusterConfig()
		if err != nil {
			// Not running in cluster -- skip gracefully.
			return nil
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to create kube client for TLS ConfigMap: %w", err)
		}
		return applyTLSFromConfigMap(context.Background(), kubeClient, namespace, cmd)
	}
}
