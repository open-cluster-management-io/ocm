package kafka

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	// sourceEventsTopic is a topic for sources to publish their resource create/update/delete events, the first
	// asterisk is a wildcard for source, the second asterisk is a wildcard for cluster.
	sourceEventsTopic = "sourceevents.*.*"
	// agentEventsTopic is a topic for agents to publish their resource status update events, the first
	// asterisk is a wildcard for source, the second asterisk is a wildcard for cluster.
	agentEventsTopic = "agentevents.*.*"
	// sourceBroadcastTopic is for a source to publish its events to all agents, the asterisk is a wildcard for source.
	sourceBroadcastTopic = "sourcebroadcast.*"
	// agentBroadcastTopic is for a agent to publish its events to all sources, the asterisk is a wildcard for cluster.
	agentBroadcastTopic = "agentbroadcast.*"
)

type KafkaOptions struct {
	// BootstrapServer is the host of the Kafka broker (hostname:port).
	BootstrapServer string `json:"bootstrapServer" yaml:"bootstrapServer"`

	// CAFile is the file path to a cert file for the MQTT broker certificate authority.
	CAFile string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	// ClientCertFile is the file path to a client cert file for TLS.
	ClientCertFile string `json:"clientCertFile,omitempty" yaml:"clientCertFile,omitempty"`
	// ClientKeyFile is the file path to a client key file for TLS.
	ClientKeyFile string `json:"clientKeyFile,omitempty" yaml:"clientKeyFile,omitempty"`

	// GroupID is a string that uniquely identifies the group of consumer processes to which this consumer belongs.
	// Each different application will have a unique consumer GroupID. The default value is agentID for agent, sourceID for source
	GroupID string `json:"groupID,omitempty" yaml:"groupID,omitempty"`
}

// Listen to all the events on the default events channel
// It's important to read these events otherwise the events channel will eventually fill up
// Detail: https://github.com/cloudevents/sdk-go/blob/main/protocol/kafka_confluent/v2/protocol.go#L90
func handleProduceEvents(producerEvents chan kafka.Event, errChan chan error) {
	if producerEvents != nil {
		return
	}
	go func() {
		for e := range producerEvents {
			switch ev := e.(type) {
			case *kafka.Message:
				// The message delivery report, indicating success or failure when sending message
				if ev.TopicPartition.Error != nil {
					klog.Errorf("Delivery failed: %v", ev.TopicPartition.Error)
				}
			case kafka.Error:
				// Generic client instance-level errors, such as
				// broker connection failures, authentication issues, etc.
				errChan <- fmt.Errorf("client error %w", ev)
			}
		}
	}()
}

// BuildKafkaOptionsFromFlags builds configs from a config filepath.
func BuildKafkaOptionsFromFlags(configPath string) (*kafka.ConfigMap, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// TODO: failed to unmarshal the data to kafka.ConfigMap directly.
	// Further investigation is required to understand the reasons behind it.
	config := &KafkaOptions{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		return nil, err
	}

	if config.BootstrapServer == "" {
		return nil, fmt.Errorf("bootstrapServer is required")
	}

	if (config.ClientCertFile == "" && config.ClientKeyFile != "") ||
		(config.ClientCertFile != "" && config.ClientKeyFile == "") {
		return nil, fmt.Errorf("either both or none of clientCertFile and clientKeyFile must be set")
	}
	if config.ClientCertFile != "" && config.ClientKeyFile != "" && config.CAFile == "" {
		return nil, fmt.Errorf("setting clientCertFile and clientKeyFile requires caFile")
	}

	configMap := &kafka.ConfigMap{
		"bootstrap.servers":       config.BootstrapServer,
		"socket.keepalive.enable": true,
		// silence spontaneous disconnection logs, kafka recovers by itself.
		"log.connection.close": false,
		// https://github.com/confluentinc/librdkafka/issues/4349
		"ssl.endpoint.identification.algorithm": "none",
		// the events channel server for both producer and consumer
		"go.events.channel.size": 1000,

		// producer
		"acks":    "1",
		"retries": "0",

		// consumer
		"group.id":                   config.GroupID,
		"enable.auto.commit":         true,
		"enable.auto.offset.store":   false,
		"queued.max.messages.kbytes": 32768, // 32 MB
		"auto.offset.reset":          "earliest",
	}

	if config.ClientCertFile != "" {
		_ = configMap.SetKey("security.protocol", "ssl")
		_ = configMap.SetKey("ssl.ca.location", config.CAFile)
		_ = configMap.SetKey("ssl.certificate.location", config.ClientCertFile)
		_ = configMap.SetKey("ssl.key.location", config.ClientKeyFile)
	}

	return configMap, nil
}
