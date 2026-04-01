package grpc

import (
	"crypto/tls"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"

	pkgtls "open-cluster-management.io/sdk-go/pkg/tls"
)

type GRPCServerOptions struct {
	TLSCertFile             string        `json:"tls_cert_file" yaml:"tls_cert_file"`
	TLSKeyFile              string        `json:"tls_key_file" yaml:"tls_key_file"`
	ClientCAFile            string        `json:"client_ca_file" yaml:"client_ca_file"`
	TLSMinVersion           string        `json:"tls_min_version" yaml:"tls_min_version"`
	TLSMaxVersion           string        `json:"tls_max_version" yaml:"tls_max_version"`
	CipherSuites            string        `json:"cipher_suites" yaml:"cipher_suites"`
	ServerBindPort          string        `json:"server_bind_port" yaml:"server_bind_port"`
	MaxConcurrentStreams    uint32        `json:"max_concurrent_streams" yaml:"max_concurrent_streams"`
	MaxReceiveMessageSize   int           `json:"max_receive_message_size" yaml:"max_receive_message_size"`
	MaxSendMessageSize      int           `json:"max_send_message_size" yaml:"max_send_message_size"`
	WriteBufferSize         int           `json:"write_buffer_size" yaml:"write_buffer_size"`
	ReadBufferSize          int           `json:"read_buffer_size" yaml:"read_buffer_size"`
	ConnectionTimeout       time.Duration `json:"connection_timeout" yaml:"connection_timeout"`
	MaxConnectionAge        time.Duration `json:"max_connection_age" yaml:"max_connection_age"`
	ClientMinPingInterval   time.Duration `json:"client_min_ping_interval" yaml:"client_min_ping_interval"`
	ServerPingInterval      time.Duration `json:"server_ping_interval" yaml:"server_ping_interval"`
	ServerPingTimeout       time.Duration `json:"server_ping_timeout" yaml:"server_ping_timeout"`
	PermitPingWithoutStream bool          `json:"permit_ping_without_stream" yaml:"permit_ping_without_stream"`
	CertWatchInterval       time.Duration `json:"cert_watch_interval" yaml:"cert_watch_interval"`

	// Parsed TLS settings, populated by Validate().
	tlsMinVersion  uint16
	tlsMaxVersion  uint16
	cipherSuiteIDs []uint16
}

func LoadGRPCServerOptions(configPath string) (*GRPCServerOptions, error) {
	opts := NewGRPCServerOptions()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		klog.Warningf("GRPC server config file %s does not exist. Using default options.", configPath)
		return opts, nil
	}

	grpcServerConfig, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(grpcServerConfig, opts); err != nil {
		return nil, err
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	return opts, nil
}

func NewGRPCServerOptions() *GRPCServerOptions {
	return &GRPCServerOptions{
		ClientCAFile:          "/var/run/secrets/hub/grpc/ca/ca-bundle.crt",
		TLSCertFile:           "/var/run/secrets/hub/grpc/serving-cert/tls.crt",
		TLSKeyFile:            "/var/run/secrets/hub/grpc/serving-cert/tls.key",
		TLSMinVersion:         "VersionTLS12",
		TLSMaxVersion:         "VersionTLS13",
		ServerBindPort:        "8090",
		MaxConcurrentStreams:  math.MaxUint32,
		MaxReceiveMessageSize: 1024 * 1024 * 4,
		MaxSendMessageSize:    math.MaxInt32,
		ConnectionTimeout:     120 * time.Second,
		MaxConnectionAge:      time.Duration(math.MaxInt64),
		ClientMinPingInterval: 5 * time.Second,
		ServerPingInterval:    30 * time.Second,
		ServerPingTimeout:     10 * time.Second,
		WriteBufferSize:       32 * 1024,
		ReadBufferSize:        32 * 1024,
		CertWatchInterval:     1 * time.Minute, // Default: 1 minute
	}
}

func (o *GRPCServerOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.ServerBindPort, "grpc-server-bindport", o.ServerBindPort, "gPRC server bind port")
	flags.Uint32Var(&o.MaxConcurrentStreams, "grpc-max-concurrent-streams", o.MaxConcurrentStreams, "gPRC max concurrent streams")
	flags.IntVar(&o.MaxReceiveMessageSize, "grpc-max-receive-message-size", o.MaxReceiveMessageSize, "gPRC max receive message size")
	flags.IntVar(&o.MaxSendMessageSize, "grpc-max-send-message-size", o.MaxSendMessageSize, "gPRC max send message size")
	flags.DurationVar(&o.ConnectionTimeout, "grpc-connection-timeout", o.ConnectionTimeout, "gPRC connection timeout")
	flags.DurationVar(&o.MaxConnectionAge, "grpc-max-connection-age", o.MaxConnectionAge, "A duration for the maximum amount of time connection may exist before closing")
	flags.DurationVar(&o.ClientMinPingInterval, "grpc-client-min-ping-interval", o.ClientMinPingInterval, "Server will terminate the connection if the client pings more than once within this duration")
	flags.DurationVar(&o.ServerPingInterval, "grpc-server-ping-interval", o.ServerPingInterval, "Duration after which the server pings the client if no activity is detected")
	flags.DurationVar(&o.ServerPingTimeout, "grpc-server-ping-timeout", o.ServerPingTimeout, "Duration the client waits for a response after sending a keepalive ping")
	flags.BoolVar(&o.PermitPingWithoutStream, "permit-ping-without-stream", o.PermitPingWithoutStream, "Allow keepalive pings even when there are no active streams")
	flags.IntVar(&o.WriteBufferSize, "grpc-write-buffer-size", o.WriteBufferSize, "gPRC write buffer size")
	flags.IntVar(&o.ReadBufferSize, "grpc-read-buffer-size", o.ReadBufferSize, "gPRC read buffer size")
	flags.StringVar(&o.TLSCertFile, "grpc-tls-cert-file", o.TLSCertFile, "The path to the tls.crt file")
	flags.StringVar(&o.TLSKeyFile, "grpc-tls-key-file", o.TLSKeyFile, "The path to the tls.key file")
	flags.StringVar(&o.ClientCAFile, "grpc-client-ca-file", o.ClientCAFile, "The path to the client ca file, must specify if using mtls authentication type")
	flags.DurationVar(&o.CertWatchInterval, "grpc-cert-watch-interval", o.CertWatchInterval, "Certificate watch interval for polling certificate file changes")
}

// Validate checks option ranges and cross-field constraints.
func (o *GRPCServerOptions) Validate() error {
	minVer, err := pkgtls.ParseTLSVersion(o.TLSMinVersion)
	if err != nil {
		return fmt.Errorf("invalid tls_min_version %q: %w", o.TLSMinVersion, err)
	}
	maxVer, err := pkgtls.ParseTLSVersion(o.TLSMaxVersion)
	if err != nil {
		return fmt.Errorf("invalid tls_max_version %q: %w", o.TLSMaxVersion, err)
	}
	if minVer < tls.VersionTLS12 {
		return fmt.Errorf("tls_min_version %q is lower than TLS 1.2; minimum supported is TLS 1.2", o.TLSMinVersion)
	}
	if minVer > maxVer {
		return fmt.Errorf("tls_min_version %q must be <= tls_max_version %q", o.TLSMinVersion, o.TLSMaxVersion)
	}
	o.tlsMinVersion = minVer
	o.tlsMaxVersion = maxVer
	// Validate certificate watch interval to prevent time.NewTicker panic
	if o.CertWatchInterval <= 30*time.Second {
		return fmt.Errorf("cert_watch_interval (%v) must be greater than 30 seconds", o.CertWatchInterval)
	}

	return o.parseCipherSuiteIDs()
}

// ApplyTLSFlags overrides TLS settings loaded from the config file with values
// from --tls-min-version and --tls-cipher-suites command-line flags.
// Called after LoadGRPCServerOptions so flags take precedence over the config file.
func (o *GRPCServerOptions) ApplyTLSFlags(minVersion, cipherSuites string) error {
	if minVersion != "" {
		o.TLSMinVersion = minVersion
	}
	if cipherSuites != "" {
		o.CipherSuites = cipherSuites
	}
	return o.Validate()
}

// parseCipherSuiteIDs converts the CipherSuites IANA names into uint16 IDs
// using the shared pkg/tls parsing utilities.
func (o *GRPCServerOptions) parseCipherSuiteIDs() error {
	if o.CipherSuites == "" {
		return nil
	}
	ids, unsupported := pkgtls.ParseCipherSuites(o.CipherSuites)
	if len(unsupported) > 0 {
		return fmt.Errorf("unrecognized cipher suite: %s", unsupported[0])
	}
	o.cipherSuiteIDs = ids
	return nil
}
