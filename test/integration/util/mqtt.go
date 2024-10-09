package util

import (
	"fmt"
	"log"
	"os"
	"time"

	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"gopkg.in/yaml.v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

const MQTTBrokerHost = "127.0.0.1:1883"

var mqttBroker *mochimqtt.Server

func RunMQTTBroker() error {
	// start a MQTT broker
	mqttBroker = mochimqtt.New(nil)

	// allow all connections
	if err := mqttBroker.AddHook(new(auth.AllowHook), nil); err != nil {
		return err
	}

	if err := mqttBroker.AddListener(listeners.NewTCP(
		listeners.Config{
			ID:      "mqtt-test-broker",
			Address: MQTTBrokerHost,
		})); err != nil {
		return err
	}

	go func() {
		if err := mqttBroker.Serve(); err != nil {
			log.Fatal(err)
		}
	}()

	return nil
}

func StopMQTTBroker() error {
	if mqttBroker != nil {
		return mqttBroker.Close()
	}

	return nil
}

func CreateMQTTConfigFile(configFileName, sourceID string) error {
	config := mqtt.MQTTConfig{
		BrokerHost: MQTTBrokerHost,
		Topics: &types.Topics{
			SourceEvents: fmt.Sprintf("sources/%s/clusters/+/sourceevents", sourceID),
			AgentEvents:  fmt.Sprintf("sources/%s/clusters/+/agentevents", sourceID),
		},
	}

	configData, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	if err := os.WriteFile(configFileName, configData, 0600); err != nil {
		return err
	}

	return nil
}

func NewMQTTSourceOptions(sourceID string) *mqtt.MQTTOptions {
	return &mqtt.MQTTOptions{
		KeepAlive: 60,
		PubQoS:    1,
		SubQoS:    1,
		Topics: types.Topics{
			SourceEvents:    fmt.Sprintf("sources/%s/clusters/+/sourceevents", sourceID),
			AgentEvents:     fmt.Sprintf("sources/%s/clusters/+/agentevents", sourceID),
			SourceBroadcast: "sources/+/sourcebroadcast",
		},
		Dialer: &mqtt.MQTTDialer{
			BrokerHost: MQTTBrokerHost,
			Timeout:    5 * time.Second,
		},
	}
}
