package source

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	confluentkafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/ghodss/yaml"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	sdkoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/kafka"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"

	"open-cluster-management.io/ocm/pkg/work/helper"
)

type Source interface {
	Host() string
	Hash() string
	Start(ctx context.Context) error
	Stop() error
	Workclientset() workclientset.Interface
}

type MQTTSource struct {
	mqttBroker     *mochimqtt.Server
	workClientSet  workclientset.Interface
	sourceID       string
	mqttBrokerHost string
	brokerHash     string
	configFile     string
}

func NewMQTTSource(configFile string) *MQTTSource {
	return &MQTTSource{
		sourceID:       "cloudevents-mqtt-integration-test",
		mqttBrokerHost: "127.0.0.1:1883",
		brokerHash:     helper.HubHash("127.0.0.1:1883"),
		configFile:     configFile,
	}
}

func (m *MQTTSource) Host() string {
	return m.mqttBrokerHost
}

func (m *MQTTSource) Hash() string {
	return m.brokerHash
}

func (m *MQTTSource) Start(ctx context.Context) error {
	// start a MQTT broker
	m.mqttBroker = mochimqtt.New(nil)

	// allow all connections
	if err := m.mqttBroker.AddHook(new(auth.AllowHook), nil); err != nil {
		return err
	}

	if err := m.mqttBroker.AddListener(listeners.NewTCP("mqtt-test-broker", m.mqttBrokerHost, nil)); err != nil {
		return err
	}

	go func() {
		if err := m.mqttBroker.Serve(); err != nil {
			log.Fatal(err)
		}
	}()

	// write the mqtt broker config to a file
	config := mqtt.MQTTConfig{
		BrokerHost: m.mqttBrokerHost,
		Topics: &types.Topics{
			SourceEvents: fmt.Sprintf("sources/%s/clusters/+/sourceevents", m.sourceID),
			AgentEvents:  fmt.Sprintf("sources/%s/clusters/+/agentevents", m.sourceID),
		},
	}

	configData, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.configFile, configData, 0600); err != nil {
		return err
	}

	mqttOptions, err := mqtt.BuildMQTTOptionsFromFlags(m.configFile)
	if err != nil {
		return err
	}

	sourceOptions := mqtt.NewSourceOptions(mqttOptions, fmt.Sprintf("%s-client", m.sourceID), m.sourceID)
	workClientSet, err := starSourceClient(ctx, sourceOptions)
	if err != nil {
		return err
	}

	m.workClientSet = workClientSet
	return nil
}

func (m *MQTTSource) Stop() error {
	return m.mqttBroker.Close()
}

func (m *MQTTSource) Workclientset() workclientset.Interface {
	return m.workClientSet
}

type KafkaSource struct {
	kafkaCluster    *confluentkafka.MockCluster
	workClientSet   workclientset.Interface
	sourceID        string
	bootstrapServer string
	serverHash      string
	configFile      string
}

func NewKafkaSource(configFile string) *KafkaSource {
	return &KafkaSource{
		sourceID:   "cloudevents-kafka-integration-test",
		configFile: configFile,
	}
}

func (k *KafkaSource) Host() string {
	return k.bootstrapServer
}

func (k *KafkaSource) Hash() string {
	return k.serverHash
}

func (k *KafkaSource) Start(ctx context.Context) error {
	kafkaCluster, err := confluentkafka.NewMockCluster(1)
	if err != nil {
		return err
	}

	k.kafkaCluster = kafkaCluster
	k.bootstrapServer = kafkaCluster.BootstrapServers()
	k.serverHash = helper.HubHash(k.bootstrapServer)
	// Note: to use mock kafka cluster, the topics must be created firstly
	// If new test cases is added, need to increase topics accordingly
	if err := k.kafkaCluster.CreateTopic(fmt.Sprintf("sourcebroadcast.%s", k.sourceID), 1, 1); err != nil {
		return err
	}
	if err := k.kafkaCluster.CreateTopic("agentbroadcast.cluster", 1, 1); err != nil {
		return err
	}
	for i := 1; i < 20; i++ {
		if err := k.kafkaCluster.CreateTopic(fmt.Sprintf("sourceevents.%s.kafka%d", k.sourceID, i), 1, 1); err != nil {
			return err
		}
		if err := k.kafkaCluster.CreateTopic(fmt.Sprintf("agentevents.%s.kafka%d", k.sourceID, i), 1, 1); err != nil {
			return err
		}
	}

	kafkaOptions := kafka.KafkaOptions{
		BootstrapServer: k.bootstrapServer,
	}
	optionsData, err := yaml.Marshal(kafkaOptions)
	if err != nil {
		return err
	}
	if err := os.WriteFile(k.configFile, optionsData, 0600); err != nil {
		return err
	}

	kafkaConfigmap, err := kafka.BuildKafkaOptionsFromFlags(k.configFile)
	if err != nil {
		return err
	}

	sourceOptions := kafka.NewSourceOptions(kafkaConfigmap, k.sourceID)
	workClientSet, err := starSourceClient(ctx, sourceOptions)
	if err != nil {
		return err
	}

	k.workClientSet = workClientSet
	return nil
}

func (k *KafkaSource) Stop() error {
	k.kafkaCluster.Close()
	return nil
}

func (k *KafkaSource) Workclientset() workclientset.Interface {
	return k.workClientSet
}

func starSourceClient(ctx context.Context, sourceOptions *sdkoptions.CloudEventsSourceOptions) (workclientset.Interface, error) {
	// build a source client
	workLister := &manifestWorkLister{}
	watcher := watcher.NewManifestWorkWatcher()
	cloudEventsClient, err := generic.NewCloudEventSourceClient[*workv1.ManifestWork](
		ctx,
		sourceOptions,
		workLister,
		work.ManifestWorkStatusHash,
		&ManifestCodec{},
		&ManifestBundleCodec{},
	)
	if err != nil {
		return nil, err
	}

	manifestWorkClient := newManifestWorkSourceClient(cloudEventsClient, watcher)
	workClient := &workV1ClientWrapper{ManifestWorkClient: manifestWorkClient}
	workClientSet := &workClientSetWrapper{WorkV1ClientWrapper: workClient}
	factory := workinformers.NewSharedInformerFactoryWithOptions(workClientSet, 1*time.Hour)
	informers := factory.Work().V1().ManifestWorks()
	manifestWorkLister := informers.Lister()
	workLister.Lister = manifestWorkLister
	manifestWorkClient.SetLister(manifestWorkLister)

	// start the source client
	cloudEventsClient.Subscribe(ctx, newManifestWorkStatusHandler(manifestWorkLister, watcher))

	go informers.Informer().Run(ctx.Done())

	return workClientSet, nil
}
