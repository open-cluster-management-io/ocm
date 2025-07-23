package util

import (
	"fmt"
	"time"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

func NewMQTTSourceOptions(brokerHost, sourceID string) *mqtt.MQTTOptions {
	return newMQTTOptions(brokerHost, types.Topics{
		SourceEvents: fmt.Sprintf("sources/%s/consumers/+/sourceevents", sourceID),
		AgentEvents:  fmt.Sprintf("sources/%s/consumers/+/agentevents", sourceID),
	})
}

func NewMQTTSourceOptionsWithSourceBroadcast(brokerHost, sourceID string) *mqtt.MQTTOptions {
	return newMQTTOptions(brokerHost, types.Topics{
		SourceEvents:    fmt.Sprintf("sources/%s/consumers/+/sourceevents", sourceID),
		AgentEvents:     fmt.Sprintf("sources/%s/consumers/+/agentevents", sourceID),
		SourceBroadcast: "sources/+/sourcebroadcast",
	})
}

func NewMQTTAgentOptions(brokerHost, sourceID, clusterName string) *mqtt.MQTTOptions {
	return newMQTTOptions(brokerHost, types.Topics{
		SourceEvents: fmt.Sprintf("sources/%s/consumers/%s/sourceevents", sourceID, clusterName),
		AgentEvents:  fmt.Sprintf("sources/%s/consumers/%s/agentevents", sourceID, clusterName),
	})
}

func NewMQTTAgentOptionsWithSourceBroadcast(brokerHost, sourceID, clusterName string) *mqtt.MQTTOptions {
	return newMQTTOptions(brokerHost, types.Topics{
		SourceEvents:    fmt.Sprintf("sources/%s/consumers/%s/sourceevents", sourceID, clusterName),
		AgentEvents:     fmt.Sprintf("sources/%s/consumers/%s/agentevents", sourceID, clusterName),
		SourceBroadcast: "sources/+/sourcebroadcast",
	})
}

func newMQTTOptions(brokerHost string, topics types.Topics) *mqtt.MQTTOptions {
	return &mqtt.MQTTOptions{
		KeepAlive: 60,
		PubQoS:    1,
		SubQoS:    1,
		Topics:    topics,
		Dialer: &mqtt.MQTTDialer{
			BrokerHost: brokerHost,
			Timeout:    5 * time.Second,
		},
	}
}
