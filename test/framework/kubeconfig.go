package framework

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

func (spoke *Spoke) DeleteExternalKubeconfigSecret(klusterlet *operatorapiv1.Klusterlet) error {
	agentNamespace := helpers.AgentNamespace(klusterlet)
	err := spoke.KubeClient.CoreV1().Secrets(agentNamespace).Delete(context.TODO(),
		helpers.ExternalManagedKubeConfig, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("failed to delete external managed secret in ns %v. %v", agentNamespace, err)
		return err
	}

	return nil
}

func (spoke *Spoke) CreateFakeExternalKubeconfigSecret(klusterlet *operatorapiv1.Klusterlet) error {
	agentNamespace := helpers.AgentNamespace(klusterlet)
	klog.Infof("klusterlet: %s/%s, \t, \t agent namespace: %s",
		klusterlet.Name, klusterlet.Namespace, agentNamespace)

	bsSecret, err := spoke.KubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(),
		helpers.BootstrapHubKubeConfig, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get bootstrap secret %v in ns %v. %v", bsSecret, agentNamespace, err)
		return err
	}

	// create external-managed-kubeconfig, will use the same cluster to simulate the Hosted mode.
	secret, err := changeHostOfKubeconfigSecret(*bsSecret, "https://kube-apiserver.i-am-a-fake-server:6443")
	if err != nil {
		klog.Errorf("failed to change host of the kubeconfig secret in. %v", err)
		return err
	}
	secret.Namespace = agentNamespace
	secret.Name = helpers.ExternalManagedKubeConfig
	secret.ResourceVersion = ""

	_, err = spoke.KubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create external managed secret %v in ns %v. %v", bsSecret, agentNamespace, err)
		return err
	}

	return nil
}

func changeHostOfKubeconfigSecret(secret corev1.Secret, apiServerURL string) (*corev1.Secret, error) {
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig not found")
	}

	if kubeconfigData == nil {
		return nil, fmt.Errorf("failed to get kubeconfig from secret: %s", secret.GetName())
	}

	kubeconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from secret: %s", secret.GetName())
	}

	if len(kubeconfig.Clusters) == 0 {
		return nil, fmt.Errorf("there is no cluster in kubeconfig from secret: %s", secret.GetName())
	}

	for k := range kubeconfig.Clusters {
		kubeconfig.Clusters[k].Server = apiServerURL
	}

	newKubeconfig, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to write new kubeconfig to secret: %s", secret.GetName())
	}

	secret.Data = map[string][]byte{
		"kubeconfig": newKubeconfig,
	}

	klog.Infof("Set the cluster server URL in %s secret with apiServerURL %s", secret.Name, apiServerURL)
	return &secret, nil
}
