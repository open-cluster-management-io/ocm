package source

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ghodss/yaml"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

const (
	sourceID       = "cloudevents-mqtt-integration-test"
	mqttBrokerHost = "127.0.0.1:1883"
)

var mqttBroker *mochimqtt.Server

type Source interface {
	Host() string
	Start(ctx context.Context) error
	Stop() error
	Workclientset() workclientset.Interface
}

type MQTTSource struct {
	configFile    string
	workClientSet workclientset.Interface
}

func NewMQTTSource(configFile string) *MQTTSource {
	return &MQTTSource{
		configFile: configFile,
	}
}

func (m *MQTTSource) Host() string {
	return mqttBrokerHost
}

func (m *MQTTSource) Start(ctx context.Context) error {
	// start a MQTT broker
	mqttBroker = mochimqtt.New(nil)

	// allow all connections
	if err := mqttBroker.AddHook(new(auth.AllowHook), nil); err != nil {
		return err
	}

	if err := mqttBroker.AddListener(listeners.NewTCP("mqtt-test-broker", mqttBrokerHost, nil)); err != nil {
		return err
	}

	go func() {
		if err := mqttBroker.Serve(); err != nil {
			log.Fatal(err)
		}
	}()

	// write the mqtt broker config to a file
	config := mqtt.MQTTConfig{
		BrokerHost: mqttBrokerHost,
	}

	configData, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.configFile, configData, 0600); err != nil {
		return err
	}

	// build a source client
	workLister := &manifestWorkLister{}
	watcher := watcher.NewManifestWorkWatcher()
	mqttOptions, err := mqtt.BuildMQTTOptionsFromFlags(m.configFile)
	if err != nil {
		return err
	}
	cloudEventsClient, err := generic.NewCloudEventSourceClient[*workv1.ManifestWork](
		ctx,
		mqtt.NewSourceOptions(mqttOptions, fmt.Sprintf("%s-client", sourceID), sourceID),
		workLister,
		work.ManifestWorkStatusHash,
		&ManifestCodec{},
	)
	if err != nil {
		return err
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
	m.workClientSet = workClientSet

	go informers.Informer().Run(ctx.Done())

	return nil
}

func (m *MQTTSource) Stop() error {
	return mqttBroker.Close()
}

func (m *MQTTSource) Workclientset() workclientset.Interface {
	return m.workClientSet
}
