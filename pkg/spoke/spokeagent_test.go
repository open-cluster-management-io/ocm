package spoke

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestGetOrGenerateClusterAgentNames(t *testing.T) {
	fakeKubeClient := kubefake.NewSimpleClientset()

	namespace := "default"
	name := "hub-kubeconfig-secret"

	// generate cluster/agent name without cluster name override
	getOrGenerateClusterAgentNames("", namespace, name, fakeKubeClient.CoreV1())

	secret, err := fakeKubeClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if secret.Data == nil {
		t.Error("cluster/agent names are not generated")
	}

	clusterName := string(secret.Data[clusterNameSecretDataKey])
	if clusterName == "" {
		t.Error("cluster name is not generated")
	}

	agentName := string(secret.Data[agentNameSecretDataKey])
	if agentName == "" {
		t.Error("agent name is not generated")
	}

	// call getOrGenerateClusterAgentNames() another time and see if the saved cluster/agent names are reused
	getOrGenerateClusterAgentNames("", namespace, name, fakeKubeClient.CoreV1())

	secret, err = fakeKubeClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if string(secret.Data[clusterNameSecretDataKey]) != clusterName {
		t.Error("cluster name saved in secret should be reused")
	}

	if string(secret.Data[agentNameSecretDataKey]) != agentName {
		t.Error("agent name saved in secret should be reused")
	}

	// call getOrGenerateClusterAgentNames() one more time with cluster name overrided and see if it works
	clusterNameOverride := "cluster0"
	getOrGenerateClusterAgentNames(clusterNameOverride, namespace, name, fakeKubeClient.CoreV1())

	secret, err = fakeKubeClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if string(secret.Data[clusterNameSecretDataKey]) != clusterNameOverride {
		t.Errorf("cluster name override %q does not take effetct", clusterNameOverride)
	}

	if string(secret.Data[agentNameSecretDataKey]) != agentName {
		t.Error("agent name saved in secret should be reused")
	}
}
