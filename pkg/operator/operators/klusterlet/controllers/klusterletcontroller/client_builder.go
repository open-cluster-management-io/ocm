/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package klusterletcontroller

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	commonrecorder "open-cluster-management.io/ocm/pkg/common/recorder"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

type managedClusterClientsBuilderInterface interface {
	withMode(mode operatorapiv1.InstallMode) managedClusterClientsBuilderInterface
	withKubeConfigSecret(namespace, name string) managedClusterClientsBuilderInterface
	build(ctx context.Context, syncCtx factory.SyncContext) (*managedClusterClients, error)
}

// managedClusterClients holds variety of kube client for managed cluster
type managedClusterClients struct {
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	// Only used for Hosted mode to generate managed cluster kubeconfig
	// with minimum permission for registration and work.
	kubeconfig                   *rest.Config
	kubeconfigSecretCreationTime metav1.Time
}

type managedClusterClientsBuilder struct {
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface

	mode            operatorapiv1.InstallMode
	secretNamespace string
	secretName      string
}

func newManagedClusterClientsBuilder(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
) *managedClusterClientsBuilder {
	return &managedClusterClientsBuilder{
		kubeClient:                kubeClient,
		apiExtensionClient:        apiExtensionClient,
		appliedManifestWorkClient: appliedManifestWorkClient,
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

func (m *managedClusterClientsBuilder) build(ctx context.Context, syncCtx factory.SyncContext) (*managedClusterClients, error) {
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
	recorderWrapper := commonrecorder.NewEventsRecorderWrapper(ctx, syncCtx.Recorder())
	_, _, err := resourceapply.ApplyNamespace(ctx, m.kubeClient.CoreV1(), recorderWrapper, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: m.secretNamespace,
			Annotations: map[string]string{
				"workload.openshift.io/allowed": "management",
			},
		},
	})
	if err != nil {
		return nil, err
	}

	creationTime, managedKubeConfig, err := getManagedKubeConfig(ctx, m.kubeClient, m.secretNamespace, m.secretName)
	if err != nil {
		return nil, err
	}

	clients := &managedClusterClients{
		kubeconfig:                   managedKubeConfig,
		kubeconfigSecretCreationTime: creationTime,
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
