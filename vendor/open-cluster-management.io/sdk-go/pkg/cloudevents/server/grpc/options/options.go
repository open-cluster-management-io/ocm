package options

import (
	"math"
	"os"
	"time"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
)

type GRPCServerOptions struct {
	TLSCertFile             string        `json:"tls_cert_file" yaml:"tls_cert_file"`
	TLSKeyFile              string        `json:"tls_key_file" yaml:"tls_key_file"`
	ClientCAFile            string        `json:"client_ca_file" yaml:"client_ca_file"`
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

	return opts, nil
}

func NewGRPCServerOptions() *GRPCServerOptions {
	return &GRPCServerOptions{
		ClientCAFile:          "/var/run/secrets/hub/grpc/ca/ca-bundle.crt",
		TLSCertFile:           "/var/run/secrets/hub/grpc/serving-cert/tls.crt",
		TLSKeyFile:            "/var/run/secrets/hub/grpc/serving-cert/tls.key",
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
}
