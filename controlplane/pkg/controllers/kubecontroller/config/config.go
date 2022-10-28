package config

import (
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	kubectrlmgrconfig "k8s.io/kubernetes/pkg/controller/apis/config"
)

// Config is the main context object for the controller manager.
type Config struct {
	ComponentConfig kubectrlmgrconfig.KubeControllerManagerConfiguration

	// the general kube client
	Client *clientset.Clientset

	// the rest config for the master
	Kubeconfig *restclient.Config

	EventBroadcaster record.EventBroadcaster
	EventRecorder    record.EventRecorder
}

type completedConfig struct {
	*Config
}

// CompletedConfig same as Config, just to swap private object.
type CompletedConfig struct {
	// Embed a private pointer that cannot be instantiated outside of this package.
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (c *Config) Complete() *CompletedConfig {
	cc := completedConfig{c}
	return &CompletedConfig{&cc}
}
