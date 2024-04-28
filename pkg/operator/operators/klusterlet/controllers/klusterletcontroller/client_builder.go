/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package klusterletcontroller

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

type managedClusterClientsBuilderInterface interface {
	withMode(mode operatorapiv1.InstallMode) managedClusterClientsBuilderInterface
	withKubeConfigSecret(namespace, name string) managedClusterClientsBuilderInterface
	build(ctx context.Context) (*managedClusterClients, error)
}

// managedClusterClients holds variety of kube client for managed cluster
type managedClusterClients struct {
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	// Only used for Hosted mode to generate managed cluster kubeconfig
	// with minimum permission for registration and work.
	kubeconfig *rest.Config
}

type managedClusterClientsBuilder struct {
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	recorder                  events.Recorder

	mode            operatorapiv1.InstallMode
	secretNamespace string
	secretName      string
}

func newManagedClusterClientsBuilder(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	recorder events.Recorder,
) *managedClusterClientsBuilder {
	return &managedClusterClientsBuilder{
		kubeClient:                kubeClient,
		apiExtensionClient:        apiExtensionClient,
		appliedManifestWorkClient: appliedManifestWorkClient,
		recorder:                  recorder,
	}
}

func (m *managedClusterClientsBuilder) withMode(mode operatorapiv1.InstallMode) managedClusterClientsBuilderInterface {
	m.mode = mode
	return m
}

func (m *managedClusterClientsBuilder) withKubeConfigSecret(namespace, name string) managedClusterClientsBuilderInterface {
	m.secretNamespace = namespace
	m.secretName = name
	return m
}

func (m *managedClusterClientsBuilder) build(ctx context.Context) (*managedClusterClients, error) {
	if !helpers.IsHosted(m.mode) {
		return &managedClusterClients{
			kubeClient:                m.kubeClient,
			apiExtensionClient:        m.apiExtensionClient,
			appliedManifestWorkClient: m.appliedManifestWorkClient,
		}, nil
	}

	// Ensure the agent namespace for users to create the external-managed-kubeconfig secret in this
	// namespace, so that in the next reconcile loop the controller can get the secret successfully after
	// the secret was created.
	if err := ensureAgentNamespace(ctx, m.kubeClient, m.secretNamespace, m.recorder); err != nil {
		return nil, err
	}

	managedKubeConfig, err := getKubeConfig(ctx, m.kubeClient, m.secretNamespace, m.secretName)
	if err != nil {
		return nil, err
	}

	clients := &managedClusterClients{
		kubeconfig: managedKubeConfig,
	}

	if clients.kubeClient, err = kubernetes.NewForConfig(managedKubeConfig); err != nil {
		return nil, err
	}
	if clients.apiExtensionClient, err = apiextensionsclient.NewForConfig(managedKubeConfig); err != nil {
		return nil, err
	}
	workClient, err := workclientset.NewForConfig(managedKubeConfig)
	if err != nil {
		return nil, err
	}
	clients.appliedManifestWorkClient = workClient.WorkV1().AppliedManifestWorks()
	return clients, nil
}

// getKubeConfig is a helper func to get kubeconfig from a secret
func getKubeConfig(ctx context.Context, kubeClient kubernetes.Interface,
	namespace, secretName string) (*rest.Config, error) {
	kubeconfigSecret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return helpers.LoadClientConfigFromSecret(kubeconfigSecret)
}

type hubClientBuilderInterface interface {
	build(ctx context.Context, secretNamespace, secretName string) (kubernetes.Interface, error)
}

type hubClientBuilder struct {
	kubeClient kubernetes.Interface
}

func newHubClientBuilder(kubeClient kubernetes.Interface) hubClientBuilderInterface {
	return &hubClientBuilder{
		kubeClient: kubeClient,
	}
}

func (b *hubClientBuilder) build(ctx context.Context, secretNamespace, secretName string) (
	kubernetes.Interface, error) {
	kubeconfig, err := getKubeConfig(ctx, b.kubeClient, secretNamespace, secretName)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(kubeconfig)
}
