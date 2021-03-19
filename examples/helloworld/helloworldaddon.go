package main

import (
	"github.com/open-cluster-management/addon-framework/pkg/agent"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type helloWorldAgent struct{}

func (h *helloWorldAgent) Manifests(cluster *clusterv1.ManagedCluster) ([]runtime.Object, error) {
	return []runtime.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "hello-world",
				Namespace: "default",
			},
		},
	}, nil
}

func (h *helloWorldAgent) GetAgentAddonOptions() *agent.AgentAddonOptions {
	return &agent.AgentAddonOptions{
		AddonName:             "helloworld",
		AddonInstallNamespace: "default",
	}
}

func (h *helloWorldAgent) GetRegistrationOption() *agent.RegistrationOption {
	// return nil since registration is not needed by this agent
	return nil
}
