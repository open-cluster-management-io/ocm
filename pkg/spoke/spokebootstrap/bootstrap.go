package spokebootstrap

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	certificatesv1beta1 "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog"
)

const (
	certPairNamePrefix      = "spoke-cluster-client"
	kubeconfigSecretDataKey = "kubeconfig"
	agentNameSecretDataKey  = "agent-name"
)

var (
	splitMetaNamespaceKey = cache.SplitMetaNamespaceKey
)

// Bootstrap registers the spoke cluster and bootstraps the agent
func Bootstrap(coreClient corev1client.CoreV1Interface, o *Options) (*restclient.Config, func(), error) {
	agentName, bootstrapped, err := recoverAgentState(coreClient, o)
	if err != nil {
		return nil, nil, err
	}

	if !bootstrapped {
		if agentName == "" {
			agentName, err = generateAgentName("")
			if err != nil {
				return nil, nil, fmt.Errorf("unable to generate agent name: %v", err)
			}
			klog.V(4).Infof("Agent name is generated: %s", agentName)
			err = writeAgentName(agentName, o.CertStoreSecret, coreClient)
			if err != nil {
				return nil, nil, err
			}
		}

		err = bootstrapAgent(agentName, coreClient, o)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to bootstrap agent: %v", err)
		}
	}

	clientConfig, err := loadRESTClientConfig(o.HubKubeconfigSecret, coreClient)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid kubeconfig: %v", err)
	}

	clusterName, err := getClusterName(agentName)
	if err != nil {
		return nil, nil, err
	}

	// block until the spoke cluster on hub is approved
	err = waitForSpokeClusterApproval(clientConfig, clusterName)
	if err != nil {
		return nil, nil, err
	}

	return rotateCertificates(agentName, clientConfig, coreClient, o)
}

// recoverAgentState recovers the current state of the cluster agent
func recoverAgentState(coreClient corev1client.CoreV1Interface, o *Options) (string, bool, error) {
	agentName, err := resolveAgentName(o.CertStoreSecret, o.ClusterNameOverride, coreClient)
	if err != nil {
		return "", false, fmt.Errorf("unable to resolve agent name: %v", err)
	}
	if agentName != "" {
		klog.V(4).Infof("Agent name is resolved: %s", agentName)
	}

	kubeconfigData, exists, err := loadKubeconfig(o.HubKubeconfigSecret, coreClient)
	if err != nil {
		return agentName, false, fmt.Errorf("unable to load kubeconfig from secret %q: %v", o.HubKubeconfigSecret, err)
	}

	if !exists {
		klog.Info("No kubeconfig is found, bootstrap is required")
		return agentName, false, nil
	}

	ok, err := isClientConfigStillValid(kubeconfigData, agentName)
	if err != nil {
		return agentName, false, err
	}
	if !ok {
		klog.Info("Kubeconfig is no long valid, bootstrap is required")
		return agentName, false, nil
	}

	klog.Info("Kubeconfig exists and is valid, skipping bootstrap")
	if agentName == "" {
		agentName, err = getAgentNameFromKubeconfig(kubeconfigData)
		if err != nil {
			return agentName, false, fmt.Errorf("unable to get agent name from kubeconfig: %v", err)
		}
		klog.V(4).Infof("Agent name is detected in certification from kubeconfig: %s", agentName)
		err = writeAgentName(agentName, o.CertStoreSecret, coreClient)
		if err != nil {
			return agentName, false, err
		}
	}

	// register spoke cluster in case it does not exists
	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return agentName, false, err
	}
	clusterName, err := getClusterName(agentName)
	if err != nil {
		return agentName, false, err
	}
	err = registerSpokeCluster(clientConfig, clusterName)
	if err != nil {
		return agentName, false, fmt.Errorf("unable to register spoke cluster %q: %v", clusterName, err)
	}

	return agentName, true, nil
}

// bootstrapAgent bootstraps cluster agent with the bootstrap kubeconfig
func bootstrapAgent(agentName string, coreClient corev1client.CoreV1Interface, o *Options) error {
	klog.Info("Start bootstrapping")
	store, err := NewSecretStore(o.CertStoreSecret, certPairNamePrefix, coreClient, nil, nil, nil)
	if err != nil {
		return fmt.Errorf("unable to create cert store")
	}

	bootstrapClientConfig, err := loadRESTClientConfig(o.BootstrapKubeconfigSecret, coreClient)
	if err != nil {
		return fmt.Errorf("unable to load kubeconfig from secret %q: %v", o.BootstrapKubeconfigSecret, err)
	}

	clusterName, err := getClusterName(agentName)
	if err != nil {
		return err
	}
	// register spoke cluster
	err = registerSpokeCluster(bootstrapClientConfig, clusterName)
	if err != nil {
		return fmt.Errorf("unable to register spoke cluster %q: %v", clusterName, err)
	}

	bootstrapClient, err := certificatesv1beta1.NewForConfig(bootstrapClientConfig)
	if err != nil {
		return fmt.Errorf("unable to create certificates signing request client: %v", err)
	}

	var keyData []byte
	if cert, err := store.Current(); err == nil {
		if cert.PrivateKey != nil {
			keyData, err = keyutil.MarshalPrivateKeyToPEM(cert.PrivateKey)
			if err != nil {
				keyData = nil
			}
		}
	}

	// Cache the private key in cert store until CSR succeeds.
	if !verifyKeyData(keyData) {
		// Note: always call GetOrGenerateTmpPrivateKey so that private key is
		// reused on next startup if CSR request fails.
		keyData, err = store.GetOrGenerateTmpPrivateKey()
		if err != nil {
			return err
		}
	}

	certData, err := requestClusterCertificate(bootstrapClient.CertificateSigningRequests(), keyData, clusterName, agentName)
	if err != nil {
		return err
	}
	klog.V(4).Info("Client certificate issued")

	if _, err := store.Update(certData, keyData); err != nil {
		return err
	}
	if err := store.RemoveTmpPrivateKey(); err != nil {
		klog.V(4).Infof("Failed cleaning up private key in cert store: %v", err)
	}

	err = writeKubeconfig(bootstrapClientConfig, certData, keyData, o.HubKubeconfigSecret, coreClient)
	if err != nil {
		return err
	}

	klog.Info("Bootstrap done")
	return nil
}

// rotateCertificates rotates certificates before client certificate becomes expired.
func rotateCertificates(agentName string, clientConfig *restclient.Config, coreClient corev1client.CoreV1Interface, o *Options) (*restclient.Config, func(), error) {
	store, err := NewSecretStore(o.CertStoreSecret, certPairNamePrefix, coreClient, clientConfig.CertData, clientConfig.KeyData,
		func(certData, keyData []byte) error {
			return writeKubeconfig(restclient.AnonymousClientConfig(clientConfig),
				certData, keyData, o.HubKubeconfigSecret, coreClient)
		})
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create cert store")
	}

	clientCertificateManager, err := NewManager(clientConfig, agentName, store)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create certificate manager")
	}

	// the rotating transport will use the cert from the cert manager
	transportConfig := restclient.AnonymousClientConfig(clientConfig)
	closeAllConns, err := UpdateTransport(wait.NeverStop, transportConfig, clientCertificateManager, 5*time.Minute)
	if err != nil {
		return nil, nil, err
	}

	klog.Info("Starting client certificate rotation.")
	clientCertificateManager.Start()

	return transportConfig, closeAllConns, nil
}

// registerSpokeCluster register a spoke cluster on hub with the given name if it does not exists yet
func registerSpokeCluster(clientConfig *restclient.Config, clusterName string) error {
	// TODO register the spoke cluster
	return nil
}

// waitForSpokeClusterApproval waits until the spoke cluster is approved on hub
func waitForSpokeClusterApproval(clientConfig *restclient.Config, clusterName string) error {
	// TODO wait until the spoke cluster is approved on hub
	return nil
}

// verifyKeyData returns true if the provided data appears to be a valid private key.
func verifyKeyData(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	_, err := keyutil.ParsePrivateKeyPEM(data)
	return err == nil
}

// resolveAgentName resolves the name of the agent.
func resolveAgentName(secretKey, clusterNameOverride string, coreClient corev1client.CoreV1Interface) (string, error) {
	agentName, err := getAgentName(secretKey, coreClient)
	if err != nil {
		return "", err
	}

	clusterName, err := getClusterName(agentName)
	if err != nil {
		return "", err
	}

	if clusterNameOverride != "" && clusterNameOverride != clusterName {
		agentName, err = generateAgentName(clusterNameOverride)
		if err != nil {
			return "", err
		}

		err := writeAgentName(agentName, secretKey, coreClient)
		if err != nil {
			return "", err
		}
		return agentName, nil
	}

	return agentName, nil
}

// generateClusterName generates a random name for cluster or return cluster UID if it's an openshift cluster
func generateClusterName() (string, error) {
	// TODO add logic to generate random cluster name
	return "cluster0", nil
}

// generateAgentName generates a random name for cluster agent
func generateAgentName(clusterName string) (string, error) {
	// TODO add logic to generate random agent name
	if clusterName == "" {
		var err error
		clusterName, err = generateClusterName()
		if err != nil {
			return "", err
		}
	}
	return clusterName + ":agent0", nil
}

// getClusterName returns cluster name by parsing agent name
func getClusterName(agentName string) (string, error) {
	if agentName == "" {
		return "", nil
	}

	names := strings.Split(agentName, ":")
	if len(names) != 2 {
		return "", fmt.Errorf("invalid agent name %q", agentName)
	}

	return names[0], nil
}

// getAgentName return agent name stored in secret
func getAgentName(secretKey string, coreClient corev1client.CoreV1Interface) (string, error) {
	namespace, name, err := splitMetaNamespaceKey(secretKey)
	if err != nil {
		return "", fmt.Errorf("invalid secret name %q: %v", secretKey, err)
	}

	secret, err := coreClient.Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("unable to get secret %q: %v", secretKey, err)
	}

	if secret.Data == nil {
		return "", nil
	}
	if value, ok := secret.Data[agentNameSecretDataKey]; ok {
		return string(value), nil
	}

	return "", nil
}

// writeAgentName saves agent name in secret
func writeAgentName(agentName, secretKey string, coreClient corev1client.CoreV1Interface) error {
	namespace, name, err := splitMetaNamespaceKey(secretKey)
	if err != nil {
		return fmt.Errorf("invalid secret name %q: %v", secretKey, err)
	}

	found := true
	secret, err := coreClient.Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			found = false
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
			}
		} else {
			return fmt.Errorf("unable to get secret %q: %v", secretKey, err)
		}
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	agentNameData := []byte(agentName)
	if value, ok := secret.Data[agentNameSecretDataKey]; ok {
		if reflect.DeepEqual(value, agentNameData) {
			return nil
		}
	}
	secret.Data[agentNameSecretDataKey] = agentNameData

	// create/update secret
	if found {
		_, err = coreClient.Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})

	} else {
		_, err = coreClient.Secrets(namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("unable to save agent name in secret %q: %v", secretKey, err)
	}
	klog.V(4).Infof("Save agent name in secret %q", secretKey)
	return nil
}
