package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"

	cloudeventsmqtt "github.com/cloudevents/sdk-go/protocol/mqtt_paho/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/eclipse/paho.golang/packets"
	"github.com/eclipse/paho.golang/paho"
	"gopkg.in/yaml.v2"
)

const (
	// SpecTopic is a MQTT topic for resource spec.
	SpecTopic = "sources/+/clusters/+/spec"

	// StatusTopic is a MQTT topic for resource status.
	StatusTopic = "sources/+/clusters/+/status"

	// SpecResyncTopic is a MQTT topic for resource spec resync.
	SpecResyncTopic = "sources/clusters/+/specresync"

	// StatusResyncTopic is a MQTT topic for resource status resync.
	StatusResyncTopic = "sources/+/clusters/statusresync"
)

// MQTTOptions holds the options that are used to build MQTT client.
type MQTTOptions struct {
	BrokerHost     string
	Username       string
	Password       string
	CAFile         string
	ClientCertFile string
	ClientKeyFile  string
	KeepAlive      uint16
	PubQoS         int
	SubQoS         int
}

// MQTTConfig holds the information needed to build connect to MQTT broker as a given user.
type MQTTConfig struct {
	// BrokerHost is the host of the MQTT broker (hostname:port).
	BrokerHost string `json:"brokerHost" yaml:"brokerHost"`

	// Username is the username for basic authentication to connect the MQTT broker.
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	// Password is the password for basic authentication to connect the MQTT broker.
	Password string `json:"password,omitempty" yaml:"password,omitempty"`

	// CAFile is the file path to a cert file for the MQTT broker certificate authority.
	CAFile string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	// ClientCertFile is the file path to a client cert file for TLS.
	ClientCertFile string `json:"clientCertFile,omitempty" yaml:"clientCertFile,omitempty"`
	// ClientKeyFile is the file path to a client key file for TLS.
	ClientKeyFile string `json:"clientKeyFile,omitempty" yaml:"clientKeyFile,omitempty"`

	// KeepAlive is the keep alive time in seconds for MQTT clients, by default is 60s
	KeepAlive *uint16 `json:"keepAlive,omitempty" yaml:"keepAlive,omitempty"`
	// PubQoS is the QoS for publish, by default is 1
	PubQoS *int `json:"pubQoS,omitempty" yaml:"pubQoS,omitempty"`
	// SubQoS is the Qos for subscribe, by default is 1
	SubQoS *int `json:"subQoS,omitempty" yaml:"subQoS,omitempty"`
}

func NewMQTTOptions() *MQTTOptions {
	return &MQTTOptions{
		KeepAlive: 60,
		PubQoS:    1,
		SubQoS:    1,
	}
}

// BuildMQTTOptionsFromFlags builds configs from a config filepath.
func BuildMQTTOptionsFromFlags(configPath string) (*MQTTOptions, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	config := &MQTTConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		return nil, err
	}

	if config.BrokerHost == "" {
		return nil, fmt.Errorf("brokerHost is required")
	}

	if (config.ClientCertFile == "" && config.ClientKeyFile != "") ||
		(config.ClientCertFile != "" && config.ClientKeyFile == "") {
		return nil, fmt.Errorf("either both or none of clientCertFile and clientKeyFile must be set")
	}
	if config.ClientCertFile != "" && config.ClientKeyFile != "" && config.CAFile == "" {
		return nil, fmt.Errorf("setting clientCertFile and clientKeyFile requires caFile")
	}

	options := &MQTTOptions{
		BrokerHost:     config.BrokerHost,
		Username:       config.Username,
		Password:       config.Password,
		CAFile:         config.CAFile,
		ClientCertFile: config.ClientCertFile,
		ClientKeyFile:  config.ClientKeyFile,
		KeepAlive:      60,
		PubQoS:         1,
		SubQoS:         1,
	}

	if config.KeepAlive != nil {
		options.KeepAlive = *config.KeepAlive
	}

	if config.PubQoS != nil {
		options.PubQoS = *config.PubQoS
	}

	if config.SubQoS != nil {
		options.SubQoS = *config.SubQoS
	}

	return options, nil
}

func (o *MQTTOptions) GetNetConn() (net.Conn, error) {
	if len(o.CAFile) != 0 {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}

		caPEM, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, err
		}

		if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
			return nil, fmt.Errorf("invalid CA %s", o.CAFile)
		}

		clientCerts, err := tls.LoadX509KeyPair(o.ClientCertFile, o.ClientKeyFile)
		if err != nil {
			return nil, err
		}

		conn, err := tls.Dial("tcp", o.BrokerHost, &tls.Config{
			RootCAs:      certPool,
			Certificates: []tls.Certificate{clientCerts},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to MQTT broker %s, %v", o.BrokerHost, err)
		}

		// ensure parallel writes are thread-Safe
		return packets.NewThreadSafeConn(conn), nil
	}

	conn, err := net.Dial("tcp", o.BrokerHost)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MQTT broker %s, %v", o.BrokerHost, err)
	}

	// ensure parallel writes are thread-Safe
	return packets.NewThreadSafeConn(conn), nil
}

func (o *MQTTOptions) GetMQTTConnectOption(clientID string) *paho.Connect {
	connect := &paho.Connect{
		ClientID:   clientID,
		KeepAlive:  o.KeepAlive,
		CleanStart: true,
	}

	if len(o.Username) != 0 {
		connect.Username = o.Username
		connect.UsernameFlag = true
	}

	if len(o.Password) != 0 {
		connect.Password = []byte(o.Password)
		connect.PasswordFlag = true
	}

	return connect
}

func (o *MQTTOptions) GetCloudEventsClient(
	ctx context.Context,
	clientID string,
	errorHandler func(error),
	clientOpts ...cloudeventsmqtt.Option,
) (cloudevents.Client, error) {
	netConn, err := o.GetNetConn()
	if err != nil {
		return nil, err
	}

	config := &paho.ClientConfig{
		ClientID:      clientID,
		Conn:          netConn,
		OnClientError: errorHandler,
	}

	opts := []cloudeventsmqtt.Option{cloudeventsmqtt.WithConnect(o.GetMQTTConnectOption(clientID))}
	opts = append(opts, clientOpts...)
	protocol, err := cloudeventsmqtt.New(ctx, config, opts...)
	if err != nil {
		return nil, err
	}

	return cloudevents.NewClient(protocol)
}

// Replace the nth occurrence of old in str by new.
func replaceNth(str, old, new string, n int) string {
	i := 0
	for m := 1; m <= n; m++ {
		x := strings.Index(str[i:], old)
		if x < 0 {
			break
		}
		i += x
		if m == n {
			return str[:i] + new + str[i+len(old):]
		}
		i += len(old)
	}
	return str
}
