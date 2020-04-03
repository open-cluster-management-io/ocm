package bootstrap

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefakeclient "k8s.io/client-go/kubernetes/fake"
)

func TestRecoverAgentState(t *testing.T) {
	kubeClient := kubefakeclient.NewSimpleClientset()

	options := &Options{
		HubKubeconfigSecret:       "default/hub-kubeconfig-secret",
		BootstrapKubeconfigSecret: "default/bootstrap-kubeconfig-secret",
		CertStoreSecret:           "default/cert-store-secret",
	}

	agentName, bootstrapped, err := recoverAgentState(kubeClient.CoreV1(), options)
	if err != nil {
		t.Fatalf("failed to recover agent state: %v", err)
	}

	if agentName != "" {
		t.Fatal("agent name should be empty")
	}

	if bootstrapped {
		t.Fatal("agent should have not been bootstrapped")
	}
}

func TestRecoverAgentStateWithAgentName(t *testing.T) {
	options := &Options{
		HubKubeconfigSecret: "default/hub-kubeconfig-secret",
		CertStoreSecret:     "default/cert-store-secret",
	}

	agentName := "cluster1: agent1"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "cert-store-secret",
		},
		Data: map[string][]byte{
			agentNameSecretDataKey: []byte(agentName),
		},
	}

	kubeClient := kubefakeclient.NewSimpleClientset(secret)

	an, bootstrapped, err := recoverAgentState(kubeClient.CoreV1(), options)
	if err != nil {
		t.Fatalf("failed to recover agent state: %v", err)
	}

	if an != agentName {
		t.Fatalf("expect agent name %q, but got %q", agentName, an)
	}

	if bootstrapped {
		t.Fatal("agent should have not been bootstrapped")
	}
}

func TestResolveAgentName(t *testing.T) {
	kubeClient := kubefakeclient.NewSimpleClientset()

	agentName, err := resolveAgentName("non-existing-secret", "", kubeClient.CoreV1())
	if err != nil {
		t.Fatalf("failed to resolve agent name: %v", err)
	}

	if agentName != "" {
		t.Fatal("agent name should be empty")
	}
}

func TestResolveAgentNameWithSecret(t *testing.T) {
	namespace := "default"
	name := "cert-store-secret"

	agentName := "cluster1: agent1"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			agentNameSecretDataKey: []byte(agentName),
		},
	}
	kubeClient := kubefakeclient.NewSimpleClientset(secret)

	an, err := resolveAgentName(namespace+"/"+name, "", kubeClient.CoreV1())
	if err != nil {
		t.Fatalf("failed to resolve agent name: %v", err)
	}

	if an != agentName {
		t.Fatalf("expect agent name %q, but got %q", agentName, an)
	}
}

func TestResolveAgentNameWithSecretAndNameOverride(t *testing.T) {
	namespace := "default"
	name := "cert-store-secret"

	agentName := "cluster1: agent1"
	clusterName := "cluster0"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			agentNameSecretDataKey: []byte(agentName),
		},
	}
	kubeClient := kubefakeclient.NewSimpleClientset(secret)

	an, err := resolveAgentName(namespace+"/"+name, clusterName, kubeClient.CoreV1())
	if err != nil {
		t.Fatalf("failed to resolve agent name: %v", err)
	}

	if an == agentName {
		t.Fatal("agent should not loaded from secret")
	}

	if !strings.HasPrefix(an, clusterName) {
		t.Fatalf("expect agent name starts with cluster name %q", clusterName)
	}
}

func TestGetClusterName(t *testing.T) {
	cn, err := getClusterName("")
	if err != nil {
		t.Fatalf("failed to get cluster name from agent name: %v", err)
	}

	if cn != "" {
		t.Fatal("cluster name should be empty")
	}
}

func TestGetClusterNameWithInvalidInput(t *testing.T) {
	_, err := getClusterName("abc")
	if err == nil {
		t.Fatal("should return error when agent name is invalid")
	}
}

func TestGetClusterNameWithValidInput(t *testing.T) {
	clusterName := "cluster0"
	cn, err := getClusterName(clusterName + ":cde")
	if err != nil {
		t.Fatalf("failed to get cluster name from agent name: %v", err)
	}

	if cn != clusterName {
		t.Fatalf("expect cluster name %q, got %q", clusterName, cn)
	}
}

func TestGetAgentName(t *testing.T) {
	kubeClient := kubefakeclient.NewSimpleClientset()
	agentName, err := getAgentName("non-existing-secret", kubeClient.CoreV1())
	if err != nil {
		t.Fatalf("failed to get agent name: %v", err)
	}

	if agentName != "" {
		t.Fatal("agent name should be empty")
	}
}

func TestGetAgentNameWithSecret(t *testing.T) {
	namespace := "default"
	name := "cert-store-secret"

	agentName := "cluster1: agent1"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			agentNameSecretDataKey: []byte(agentName),
		},
	}

	kubeClient := kubefakeclient.NewSimpleClientset(secret)
	an, err := getAgentName(namespace+"/"+name, kubeClient.CoreV1())
	if err != nil {
		t.Fatalf("failed to get agent name: %v", err)
	}

	if an != agentName {
		t.Fatalf("expect agent name %q, but got %q", agentName, an)
	}
}

func TestWriteAgentName(t *testing.T) {
	kubeClient := kubefakeclient.NewSimpleClientset()
	agentName := "cluster1:agent1"
	namespace, name := "default", "cert-store-secret"
	err := writeAgentName(agentName, namespace+"/"+name, kubeClient.CoreV1())
	if err != nil {
		t.Fatalf("failed to write agent name: %v", err)
	}

	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}

	if secret.Data == nil {
		t.Fatalf("secret is empty")
	}

	value, ok := secret.Data[agentNameSecretDataKey]
	if !ok {
		t.Fatal("agent name is not writtten")
	}

	if string(value) != agentName {
		t.Fatalf("expected %q but got %q", agentName, string(value))
	}

}
