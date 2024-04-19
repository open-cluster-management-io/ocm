package work

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/kafka"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
)

const (
	ConfigTypeKube  = "kube"
	ConfigTypeMQTT  = "mqtt"
	ConfigTypeGRPC  = "grpc"
	ConfigTypeKafka = "kafka"
)

// ConfigLoader loads a configuration object with a configuration file.
type ConfigLoader struct {
	configType string
	configPath string

	kubeConfig *rest.Config
}

// NewConfigLoader returns a ConfigLoader with the given configuration type and configuration file path.
//
// Available configuration types:
//   - kube
//   - mqtt
//   - grpc
//   - kafka
func NewConfigLoader(configType, configPath string) *ConfigLoader {
	return &ConfigLoader{
		configType: configType,
		configPath: configPath,
	}
}

// WithKubeConfig sets a kube config, this config will be used when configuration type is kube and the kube
// configuration file path not set.
func (l *ConfigLoader) WithKubeConfig(kubeConfig *rest.Config) *ConfigLoader {
	l.kubeConfig = kubeConfig
	return l
}

// TODO using a specified config instead of any
func (l *ConfigLoader) LoadConfig() (string, any, error) {
	switch l.configType {
	case ConfigTypeKube:
		if l.configPath == "" {
			if l.kubeConfig == nil {
				return "", nil, fmt.Errorf("neither the kube config path nor kube config object was specified")
			}

			return l.kubeConfig.Host, l.kubeConfig, nil
		}

		kubeConfig, err := clientcmd.BuildConfigFromFlags("", l.configPath)
		if err != nil {
			return "", nil, err
		}

		return kubeConfig.Host, kubeConfig, nil
	case ConfigTypeMQTT:
		mqttOptions, err := mqtt.BuildMQTTOptionsFromFlags(l.configPath)
		if err != nil {
			return "", nil, err
		}

		return mqttOptions.BrokerHost, mqttOptions, nil
	case ConfigTypeGRPC:
		grpcOptions, err := grpc.BuildGRPCOptionsFromFlags(l.configPath)
		if err != nil {
			return "", nil, err
		}

		return grpcOptions.URL, grpcOptions, nil

	case ConfigTypeKafka:
		configMap, err := kafka.BuildKafkaOptionsFromFlags(l.configPath)
		if err != nil {
			return "", nil, err
		}
		val, err := configMap.Get("bootstrap.servers", "")
		if err != nil {
			return "", nil, err
		}
		server, ok := val.(string)
		if !ok {
			return "", nil, fmt.Errorf("failed to get kafka bootstrap.servers from configMap")
		}
		return server, configMap, nil
	}

	return "", nil, fmt.Errorf("unsupported config type %s", l.configType)
}
