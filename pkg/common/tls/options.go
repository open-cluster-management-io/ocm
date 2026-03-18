package tls

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// Options holds TLS configuration options
type Options struct {
	MinVersion   string
	CipherSuites string

	// EnableConfigMapWatch enables watching the ConfigMap for changes
	EnableConfigMapWatch bool

	// Namespace is the namespace to watch for the TLS ConfigMap
	Namespace string
}

// NewOptions creates new TLS options with defaults
func NewOptions() *Options {
	return &Options{
		EnableConfigMapWatch: true,
	}
}

// AddFlags adds TLS flags to the flag set
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.MinVersion, "tls-min-version", o.MinVersion,
		"Minimum TLS version supported. Values: VersionTLS10, VersionTLS11, VersionTLS12, VersionTLS13")
	fs.StringVar(&o.CipherSuites, "tls-cipher-suites", o.CipherSuites,
		"Comma-separated list of cipher suites. If empty, uses profile defaults or Go defaults.")
	fs.BoolVar(&o.EnableConfigMapWatch, "tls-configmap-watch", o.EnableConfigMapWatch,
		"Enable watching ConfigMap for TLS profile changes (triggers pod restart on change)")
}

// GetTLSConfig returns the TLS configuration, with the following priority:
// 1. Command-line flags (--tls-min-version, --tls-cipher-suites)
// 2. ConfigMap (ocm-tls-profile in the specified namespace)
// 3. Default (TLS 1.2)
func (o *Options) GetTLSConfig(ctx context.Context, client kubernetes.Interface) (*TLSConfig, error) {
	logger := klog.FromContext(ctx)

	// Priority 1: Check command-line flags
	flagConfig, err := TLSConfigFromFlags(o.MinVersion, o.CipherSuites)
	if err != nil {
		return nil, fmt.Errorf("invalid TLS flags: %w", err)
	}
	if flagConfig != nil {
		logger.Info("Using TLS config from command-line flags",
			"minVersion", TLSVersionToString(flagConfig.MinVersion),
			"cipherSuites", len(flagConfig.CipherSuites))
		return flagConfig, nil
	}

	// Priority 2: Check ConfigMap
	if o.Namespace != "" && client != nil {
		cmConfig, err := LoadTLSConfigFromConfigMap(ctx, client, o.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS config from ConfigMap: %w", err)
		}
		if cmConfig != nil {
			logger.Info("Using TLS config from ConfigMap",
				"namespace", o.Namespace,
				"configmap", ConfigMapName,
				"minVersion", TLSVersionToString(cmConfig.MinVersion),
				"cipherSuites", len(cmConfig.CipherSuites))
			return cmConfig, nil
		}
	}

	// Priority 3: Use default
	defaultConfig := GetDefaultTLSConfig()
	logger.Info("Using default TLS config",
		"minVersion", TLSVersionToString(defaultConfig.MinVersion))
	return defaultConfig, nil
}

// StartConfigMapWatcher starts watching the ConfigMap for changes
// When the ConfigMap changes, it cancels the context to trigger graceful shutdown
// Returns nil if watching is disabled or namespace is not set
func (o *Options) StartConfigMapWatcher(ctx context.Context, client kubernetes.Interface, cancel context.CancelFunc) error {
	if !o.EnableConfigMapWatch {
		klog.FromContext(ctx).V(4).Info("ConfigMap watching disabled")
		return nil
	}

	if o.Namespace == "" {
		klog.FromContext(ctx).V(4).Info("Namespace not set, skipping ConfigMap watch")
		return nil
	}

	if client == nil {
		return fmt.Errorf("kubernetes client is required for ConfigMap watching")
	}

	watcher := NewConfigMapWatcher(client, o.Namespace, cancel)
	return watcher.Start(ctx)
}

// GetTLSConfigForServer returns a *tls.Config suitable for HTTPS servers
func (o *Options) GetTLSConfigForServer(ctx context.Context, client kubernetes.Interface) (*tls.Config, error) {
	tlsCfg, err := o.GetTLSConfig(ctx, client)
	if err != nil {
		return nil, err
	}
	return BuildTLSConfig(tlsCfg), nil
}

// GetTLSConfigFunc returns a function suitable for controller-runtime TLSOpts
func (o *Options) GetTLSConfigFunc(ctx context.Context, client kubernetes.Interface) (func(*tls.Config), error) {
	tlsCfg, err := o.GetTLSConfig(ctx, client)
	if err != nil {
		return nil, err
	}
	return TLSConfigToFunc(tlsCfg), nil
}
