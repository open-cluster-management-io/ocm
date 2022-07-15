package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E suite")
}

var (
	managedClusterName string
	hubKubeClient      kubernetes.Interface
	hubAddOnClient     addonclient.Interface
	hubClusterClient   clusterclient.Interface

	hostedKlusterletName              string // name of the hosted mode klusterlet
	hostedManagedKubeconfigSecretName string // name of the secret for the managed cluster in hosted mode
	hostedManagedClusterName          string // name of the managed cluster in hosted mode
	hostingClusterName                string // name of the hosting cluster in hosted mode
	hostedManagedKubeClient           kubernetes.Interface
)

// This suite is sensitive to the following environment variables:
//
// - MANAGED_CLUSTER_NAME sets the name of the cluster
// - HOSTED_MANAGED_CLUSTER_NAME sets the name of the hosted managed cluster, only useful in Hosted mode
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	managedClusterName = os.Getenv("MANAGED_CLUSTER_NAME")
	if managedClusterName == "" {
		managedClusterName = "cluster1"
	}
	hostingClusterName = managedClusterName
	hostedManagedClusterName = os.Getenv("HOSTED_MANAGED_CLUSTER_NAME")
	if hostedManagedClusterName == "" {
		hostedManagedClusterName = "cluster2"
	}
	hostedKlusterletName = os.Getenv("HOSTED_MANAGED_KLUSTERLET_NAME")
	if hostedKlusterletName == "" {
		hostedKlusterletName = "managed"
	}
	hostedManagedKubeconfigSecretName = os.Getenv("HOSTED_MANAGED_KUBECONFIG_SECRET_NAME")
	if hostedManagedKubeconfigSecretName == "" {
		hostedManagedKubeconfigSecretName = "e2e-hosted-managed-kubeconfig"
	}

	err := func() error {
		var err error
		clusterCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}

		hubKubeClient, err = kubernetes.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubAddOnClient, err = addonclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubClusterClient, err = clusterclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hostedManagedKubeConfig, err := getHostedManagedKubeConfig(
			context.Background(), hubKubeClient, hostedKlusterletName, hostedManagedKubeconfigSecretName)
		if err != nil {
			return err
		}

		hostedManagedKubeClient, err = kubernetes.NewForConfig(hostedManagedKubeConfig)

		return err
	}()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

// getHostedManagedKubeConfig is a helper func for Hosted mode, it will retrieve managed cluster
// kubeconfig from "external-managed-kubeconfig" secret.
func getHostedManagedKubeConfig(ctx context.Context, kubeClient kubernetes.Interface,
	namespace, secretName string) (*rest.Config, error) {
	managedKubeconfigSecret, err := kubeClient.CoreV1().Secrets(namespace).Get(
		context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return loadClientConfigFromSecret(managedKubeconfigSecret)
}

// loadClientConfigFromSecret returns a client config loaded from the given secret
func loadClientConfigFromSecret(secret *corev1.Secret) (*rest.Config, error) {
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("unable to find kubeconfig in secret %q %q",
			secret.Namespace, secret.Name)
	}

	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, err
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return nil, fmt.Errorf("unable to find the current context %q from the kubeconfig in secret %q %q",
			config.CurrentContext, secret.Namespace, secret.Name)
	}

	if authInfo, ok := config.AuthInfos[context.AuthInfo]; ok {
		// use embeded cert/key data instead of references to external cert/key files
		if certData, ok := secret.Data["tls.crt"]; ok && len(authInfo.ClientCertificateData) == 0 {
			authInfo.ClientCertificateData = certData
			authInfo.ClientCertificate = ""
		}
		if keyData, ok := secret.Data["tls.key"]; ok && len(authInfo.ClientKeyData) == 0 {
			authInfo.ClientKeyData = keyData
			authInfo.ClientKey = ""
		}
	}

	return clientcmd.NewDefaultClientConfig(*config, nil).ClientConfig()
}
