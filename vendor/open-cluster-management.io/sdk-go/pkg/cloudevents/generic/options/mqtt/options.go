package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	cloudeventsmqtt "github.com/cloudevents/sdk-go/protocol/mqtt_paho/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/eclipse/paho.golang/packets"
	"github.com/eclipse/paho.golang/paho"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/util/errors"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// MQTTOptions holds the options that are used to build MQTT client.
type MQTTOptions struct {
	Topics         types.Topics
	BrokerHost     string
	Username       string
	Password       string
	CAFile         string
	ClientCertFile string
	ClientKeyFile  string
	KeepAlive      uint16
	Timeout        time.Duration
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

	// Topics are MQTT topics for resource spec, status and resync.
	Topics *types.Topics `json:"topics,omitempty" yaml:"topics,omitempty"`
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

	if err := validateTopics(config.Topics); err != nil {
		return nil, err
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
		Timeout:        180 * time.Second,
		Topics:         *config.Topics,
	}

	if config.KeepAlive != nil {
		options.KeepAlive = *config.KeepAlive
		// Setting the mqtt tcp connection read and write timeouts to three times the mqtt keepalive
		options.Timeout = 3 * time.Duration(*config.KeepAlive) * time.Second
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
	err = netConn.SetDeadline(time.Now().Add(o.Timeout))
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

func validateTopics(topics *types.Topics) error {
	if topics == nil {
		return fmt.Errorf("the topics must be set")
	}

	var errs []error
	if !regexp.MustCompile(types.SourceEventsTopicPattern).MatchString(topics.SourceEvents) {
		errs = append(errs, fmt.Errorf("invalid source events topic %q, it should match `%s`",
			topics.SourceEvents, types.SourceEventsTopicPattern))
	}

	if !regexp.MustCompile(types.AgentEventsTopicPattern).MatchString(topics.AgentEvents) {
		errs = append(errs, fmt.Errorf("invalid agent events topic %q, it should match `%s`",
			topics.AgentEvents, types.AgentEventsTopicPattern))
	}

	if len(topics.SourceBroadcast) != 0 {
		if !regexp.MustCompile(types.SourceBroadcastTopicPattern).MatchString(topics.SourceBroadcast) {
			errs = append(errs, fmt.Errorf("invalid source broadcast topic %q, it should match `%s`",
				topics.SourceBroadcast, types.SourceBroadcastTopicPattern))
		}
	}

	if len(topics.AgentBroadcast) != 0 {
		if !regexp.MustCompile(types.AgentBroadcastTopicPattern).MatchString(topics.AgentBroadcast) {
			errs = append(errs, fmt.Errorf("invalid agent broadcast topic %q, it should match `%s`",
				topics.AgentBroadcast, types.AgentBroadcastTopicPattern))
		}
	}

	return errors.NewAggregate(errs)
}

func getSourceFromEventsTopic(topic string) (string, error) {
	if !regexp.MustCompile(types.EventsTopicPattern).MatchString(topic) {
		return "", fmt.Errorf("failed to get source from topic: %q", topic)
	}

	subTopics := strings.Split(topic, "/")
	// get source form share topic, e.g. $share/group/sources/+/consumers/+/agentevents
	if strings.HasPrefix(topic, "$share") {
		return subTopics[3], nil
	}

	// get source form topic, e.g. sources/+/consumers/+/agentevents
	return subTopics[1], nil
}

func replaceLast(str, old, new string) string {
	last := strings.LastIndex(str, old)
	if last == -1 {
		return str
	}
	return str[:last] + new + str[last+len(old):]
}
