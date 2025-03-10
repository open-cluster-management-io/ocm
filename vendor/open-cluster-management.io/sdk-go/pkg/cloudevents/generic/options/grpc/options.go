package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/keepalive"
	"gopkg.in/yaml.v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/cert"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
)

var _ cert.Connection = &GRPCDialer{}

// GRPCDialer is a gRPC dialer that connects to a gRPC server
// with the given URL, TLS configuration and keepalive options.
type GRPCDialer struct {
	URL              string
	KeepAliveOptions KeepAliveOptions
	TLSConfig        *tls.Config
	TokenFile        string
	conn             *grpc.ClientConn
}

// KeepAliveOptions holds the keepalive options for the gRPC client.
type KeepAliveOptions struct {
	Enable              bool
	Time                time.Duration
	Timeout             time.Duration
	PermitWithoutStream bool
}

// Dial connects to the gRPC server and returns a gRPC client connection.
func (d *GRPCDialer) Dial() (*grpc.ClientConn, error) {
	// Prepare gRPC dial options.
	dialOpts := []grpc.DialOption{}
	if d.KeepAliveOptions.Enable {
		dialOpts = append(dialOpts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                d.KeepAliveOptions.Time,
			Timeout:             d.KeepAliveOptions.Timeout,
			PermitWithoutStream: d.KeepAliveOptions.PermitWithoutStream,
		}))
	}
	if d.TLSConfig != nil {
		// Enable TLS
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(d.TLSConfig)))
		if len(d.TokenFile) != 0 {
			// Use token-based authentication if token file is provided.
			token, err := os.ReadFile(d.TokenFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read token file %s, %v", d.TokenFile, err)
			}
			perRPCCred := oauth.TokenSource{
				TokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: string(token),
				})}
			// Add per-RPC credentials to the dial options.
			dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(perRPCCred))
		}

		// Establish a TLS connection to the gRPC server.
		conn, err := grpc.Dial(d.URL, dialOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to grpc server %s, %v", d.URL, err)
		}

		// Cache the connection for future use.
		d.conn = conn
		return d.conn, nil
	}

	// Insecure connection option; should not be used in production.
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(d.URL, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to grpc server %s, %v", d.URL, err)
	}

	// Cache the connection for future use.
	d.conn = conn
	return d.conn, nil
}

// Close closes the gRPC client connection.
func (d *GRPCDialer) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

// GRPCOptions holds the options that are used to build gRPC client.
type GRPCOptions struct {
	Dialer *GRPCDialer
}

// GRPCConfig holds the information needed to build connect to gRPC server as a given user.
type GRPCConfig struct {
	// URL is the address of the gRPC server (host:port).
	URL string `json:"url" yaml:"url"`
	// CAFile is the file path to a cert file for the gRPC server certificate authority.
	CAFile string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	// ClientCertFile is the file path to a client cert file for TLS.
	ClientCertFile string `json:"clientCertFile,omitempty" yaml:"clientCertFile,omitempty"`
	// ClientKeyFile is the file path to a client key file for TLS.
	ClientKeyFile string `json:"clientKeyFile,omitempty" yaml:"clientKeyFile,omitempty"`
	// TokenFile is the file path to a token file for authentication.
	TokenFile string `json:"tokenFile,omitempty" yaml:"tokenFile,omitempty"`
	// keepalive options
	KeepAliveConfig KeepAliveConfig `json:"keepAliveConfig,omitempty" yaml:"keepAliveConfig,omitempty"`
}

// KeepAliveConfig holds the keepalive options for the gRPC client.
type KeepAliveConfig struct {
	// Enable specifies whether the keepalive option is enabled.
	// When disabled, other keepalive configurations are ignored. Default is false.
	Enable bool `json:"enable,omitempty" yaml:"enable,omitempty"`
	// Time sets the duration after which the client pings the server if no activity is seen.
	// A minimum value of 10s is enforced if set below that. Default is 30s.
	Time *time.Duration `json:"time,omitempty" yaml:"time,omitempty"`

	// Timeout sets the duration the client waits for a response after a keepalive ping.
	// If no response is received, the connection is closed. Default is 10s.
	Timeout *time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// PermitWithoutStream determines if keepalive pings are sent when there are no active RPCs.
	// If false, pings are not sent and Time and Timeout are ignored. Default is false.
	PermitWithoutStream bool `json:"permitWithoutStream,omitempty" yaml:"permitWithoutStream,omitempty"`
}

// BuildGRPCOptionsFromFlags builds configs from a config filepath.
func BuildGRPCOptionsFromFlags(configPath string) (*GRPCOptions, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	config := &GRPCConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		return nil, err
	}

	if config.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	if (config.ClientCertFile == "" && config.ClientKeyFile != "") ||
		(config.ClientCertFile != "" && config.ClientKeyFile == "") {
		return nil, fmt.Errorf("either both or none of clientCertFile and clientKeyFile must be set")
	}
	if config.ClientCertFile != "" && config.ClientKeyFile != "" && config.CAFile == "" {
		return nil, fmt.Errorf("setting clientCertFile and clientKeyFile requires caFile")
	}
	if config.TokenFile != "" && config.CAFile == "" {
		return nil, fmt.Errorf("setting tokenFile requires caFile")
	}

	options := &GRPCOptions{
		Dialer: &GRPCDialer{
			URL:       config.URL,
			TokenFile: config.TokenFile,
		},
	}

	// Default keepalive options
	keepAliveOptions := KeepAliveOptions{
		Enable:              false,
		Time:                30 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: false,
	}
	keepAliveOptions.Enable = config.KeepAliveConfig.Enable
	if config.KeepAliveConfig.Time != nil {
		keepAliveOptions.Time = *config.KeepAliveConfig.Time
	}
	if config.KeepAliveConfig.Timeout != nil {
		keepAliveOptions.Timeout = *config.KeepAliveConfig.Timeout
	}
	keepAliveOptions.PermitWithoutStream = config.KeepAliveConfig.PermitWithoutStream

	// Set the keepalive options
	options.Dialer.KeepAliveOptions = keepAliveOptions

	// Set up TLS configuration for the gRPC connection, the certificates will be reloaded periodically.
	options.Dialer.TLSConfig, err = cert.AutoLoadTLSConfig(config.CAFile, config.ClientCertFile, config.ClientKeyFile, options.Dialer)
	if err != nil {
		return nil, err
	}

	return options, nil
}

func NewGRPCOptions() *GRPCOptions {
	return &GRPCOptions{}
}

func (o *GRPCOptions) GetCloudEventsProtocol(ctx context.Context, errorHandler func(error), clientOpts ...protocol.Option) (options.CloudEventsProtocol, error) {
	conn, err := o.Dialer.Dial()
	if err != nil {
		return nil, err
	}

	// Periodically (every 100ms) check the connection status and reconnect if necessary.
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				conn.Close()
			case <-ticker.C:
				connState := conn.GetState()
				// If any failure in any of the steps needed to establish connection, or any failure encountered while
				// expecting successful communication on established channel, the grpc client connection state will be
				// TransientFailure.
				// For a connected grpc client, if the connections is down, the grpc client connection state will be
				// changed from Ready to Idle.
				// When client certificate is expired, client will proactively close the connection, which will result
				// in connection state changed from Ready to Shutdown.
				if connState == connectivity.TransientFailure || connState == connectivity.Idle || connState == connectivity.Shutdown {
					errorHandler(fmt.Errorf("grpc connection is disconnected (state=%s)", connState))
					ticker.Stop()
					if connState != connectivity.Shutdown {
						// don't close the connection if it's already shutdown
						conn.Close()
					}
					return // exit the goroutine as the error handler function will handle the reconnection.
				}
			}
		}
	}()

	opts := []protocol.Option{}
	opts = append(opts, clientOpts...)
	return protocol.NewProtocol(conn, opts...)
}
