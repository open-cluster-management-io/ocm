package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
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
	Token            string
	mu               sync.Mutex       // Mutex to protect the connection.
	conn             *grpc.ClientConn // Cached gRPC client connection.
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
	// Return the cached connection if it exists and is ready.
	// Should not return a nil or unready connection, otherwise the caller (cloudevents client)
	// will not use the connection for sending and receiving events in reconnect scenarios.
	// lock the connection to ensure the connection is not created by multiple goroutines concurrently.
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil && (d.conn.GetState() == connectivity.Connecting || d.conn.GetState() == connectivity.Ready) {
		return d.conn, nil
	}
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
		if len(d.Token) != 0 {
			perRPCCred := oauth.TokenSource{
				TokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: d.Token,
				})}
			// Add per-RPC credentials to the dial options.
			dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(perRPCCred))
		}

		// Establish a TLS connection to the gRPC server.
		conn, err := grpc.NewClient(d.URL, dialOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to grpc server %s, %v", d.URL, err)
		}

		// Cache the connection for future use.
		d.conn = conn
		return d.conn, nil
	}

	// Insecure connection option; should not be used in production.
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient(d.URL, dialOpts...)

	if err != nil {
		return nil, fmt.Errorf("failed to connect to grpc server %s, %v", d.URL, err)
	}

	// Cache the connection for future use.
	d.conn = conn
	return d.conn, nil
}

// Close closes the gRPC client connection.
func (d *GRPCDialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
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
	cert.CertConfig `json:",inline" yaml:",inline"`

	// URL is the address of the gRPC server (host:port).
	URL string `json:"url" yaml:"url"`

	// TokenFile is the file path to a token file for authentication.
	TokenFile string `json:"tokenFile,omitempty" yaml:"tokenFile,omitempty"`
	// Token is the token for authentication
	Token string `json:"token" yaml:"token"`

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

func LoadConfig(configPath string) (*GRPCConfig, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	config := &GRPCConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		return nil, err
	}

	if err := config.CertConfig.EmbedCerts(); err != nil {
		return nil, err
	}

	return config, nil
}

// BuildGRPCOptionsFromFlags builds configs from a config filepath.
func BuildGRPCOptionsFromFlags(configPath string) (*GRPCOptions, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	if config.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	if err := config.CertConfig.Validate(); err != nil {
		return nil, err
	}

	token := config.Token
	if config.Token == "" && config.TokenFile != "" {
		tokenBytes, err := os.ReadFile(config.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read token file %s, %v", config.TokenFile, err)
		}
		token = string(tokenBytes)
	}
	if token != "" && len(config.CAData) == 0 {
		return nil, fmt.Errorf("setting token requires authority certificates")
	}

	options := &GRPCOptions{
		Dialer: &GRPCDialer{
			URL:   config.URL,
			Token: token,
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

	if token != "" || config.CertConfig.HasCerts() {
		// Set up TLS configuration for the gRPC connection, the certificates will be reloaded periodically.
		options.Dialer.TLSConfig, err = cert.AutoLoadTLSConfig(
			config.CertConfig,
			func() (*cert.CertConfig, error) {
				config, err := LoadConfig(configPath)
				if err != nil {
					return nil, err
				}
				return &config.CertConfig, nil
			},
			options.Dialer,
		)
		if err != nil {
			return nil, err
		}
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
		state := conn.GetState()
		for {
			if !conn.WaitForStateChange(ctx, state) {
				// the ctx is closed, stop this watch
				klog.Infof("Stop watch grpc connection state")
				return
			}

			newState := conn.GetState()
			// If any failure in any of the steps needed to establish connection, or any failure
			// encountered while expecting successful communication on established channel, the
			// grpc client connection state will be TransientFailure.
			// When client certificate is expired, client will proactively close the connection,
			// which will result in connection state changed from Ready to Shutdown.
			// When server is closed, client will NOT close or reestablish the connection proactively,
			// it will only change the connection state from Ready to Idle.
			if newState == connectivity.TransientFailure || newState == connectivity.Shutdown ||
				newState == connectivity.Idle {
				errorHandler(fmt.Errorf("grpc connection is disconnected (state=%s)", newState))
				if newState != connectivity.Shutdown {
					// don't close the connection if it's already shutdown
					if err := conn.Close(); err != nil {
						klog.Errorf("failed to close gRPC connection, %v", err)
					}
				}
				return // exit the goroutine as the error handler function will handle the reconnection.
			}

			state = newState
		}
	}()

	opts := []protocol.Option{}
	opts = append(opts, clientOpts...)
	return protocol.NewProtocol(conn, opts...)
}
