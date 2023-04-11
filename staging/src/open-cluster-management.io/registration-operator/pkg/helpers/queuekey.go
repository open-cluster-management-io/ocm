package helpers

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/library-go/pkg/controller/factory"

	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

const (
	// ClusterManagerDefaultNamespace is the default namespace of clustermanager
	ClusterManagerDefaultNamespace = "open-cluster-management-hub"
	// KlusterletDefaultNamespace is the default namespace of klusterlet
	KlusterletDefaultNamespace = "open-cluster-management-agent"
	// BootstrapHubKubeConfig is the secret name of bootstrap kubeconfig secret to connect to hub
	BootstrapHubKubeConfig = "bootstrap-hub-kubeconfig"
	// HubKubeConfig is the secret name of kubeconfig secret to connect to hub with mtls
	HubKubeConfig = "hub-kubeconfig-secret"
	// ExternalHubKubeConfig is the secret name of kubeconfig secret to connecting to the hub cluster.
	ExternalHubKubeConfig = "external-hub-kubeconfig"
	// ExternalManagedKubeConfig is the secret name of kubeconfig secret to connecting to the managed cluster
	// Only applicable to Hosted mode, klusterlet-operator uses it to install resources on the managed cluster.
	ExternalManagedKubeConfig = "external-managed-kubeconfig"
	// ExternalManagedKubeConfigRegistration is the secret name of kubeconfig secret to connecting to the managed cluster
	// Only applicable to Hosted mode, registration-agent uses it to connect to the managed cluster.
	ExternalManagedKubeConfigRegistration = "external-managed-kubeconfig-registration"
	// ExternalManagedKubeConfigWork is the secret name of kubeconfig secret to connecting to the managed cluster
	// Only applicable to Hosted mode, work-agent uses it to connect to the managed cluster.
	ExternalManagedKubeConfigWork = "external-managed-kubeconfig-work"

	RegistrationWebhookSecret  = "registration-webhook-serving-cert"
	RegistrationWebhookService = "cluster-manager-registration-webhook"
	WorkWebhookSecret          = "work-webhook-serving-cert"
	WorkWebhookService         = "cluster-manager-work-webhook"
)

func ClusterManagerNamespace(clustermanagername string, mode operatorapiv1.InstallMode) string {
	if mode == operatorapiv1.InstallModeHosted {
		return clustermanagername
	}
	return ClusterManagerDefaultNamespace
}

func KlusterletSecretQueueKeyFunc(klusterletLister operatorlister.KlusterletLister) factory.ObjectQueueKeyFunc {
	return func(obj runtime.Object) string {
		accessor, _ := meta.Accessor(obj)
		namespace := accessor.GetNamespace()
		name := accessor.GetName()
		interestedObjectFound := false
		if name == HubKubeConfig || name == BootstrapHubKubeConfig || name == ExternalManagedKubeConfig {
			interestedObjectFound = true
		}
		if !interestedObjectFound {
			return ""
		}

		klusterlets, err := klusterletLister.List(labels.Everything())
		if err != nil {
			return ""
		}

		if klusterlet := FindKlusterletByNamespace(klusterlets, namespace); klusterlet != nil {
			return klusterlet.Name
		}

		return ""
	}
}

func KlusterletDeploymentQueueKeyFunc(klusterletLister operatorlister.KlusterletLister) factory.ObjectQueueKeyFunc {
	return func(obj runtime.Object) string {
		accessor, _ := meta.Accessor(obj)
		namespace := accessor.GetNamespace()
		name := accessor.GetName()
		interestedObjectFound := false
		if strings.HasSuffix(name, "registration-agent") || strings.HasSuffix(name, "work-agent") {
			interestedObjectFound = true
		}
		if !interestedObjectFound {
			return ""
		}

		klusterlets, err := klusterletLister.List(labels.Everything())
		if err != nil {
			return ""
		}

		if klusterlet := FindKlusterletByNamespace(klusterlets, namespace); klusterlet != nil {
			return klusterlet.Name
		}

		return ""
	}
}

func ClusterManagerDeploymentQueueKeyFunc(clusterManagerLister operatorlister.ClusterManagerLister) factory.ObjectQueueKeyFunc {
	return func(obj runtime.Object) string {
		accessor, _ := meta.Accessor(obj)
		name := accessor.GetName()
		namespace := accessor.GetNamespace()
		interestedObjectFound := false

		if strings.HasSuffix(name, "registration-controller") ||
			strings.HasSuffix(name, "registration-webhook") ||
			strings.HasSuffix(name, "work-webhook") ||
			strings.HasSuffix(name, "placement-controller") {
			interestedObjectFound = true
		}
		if !interestedObjectFound {
			return ""
		}

		clustermanagers, err := clusterManagerLister.List(labels.Everything())
		if err != nil {
			return ""
		}

		clustermanager, err := FindClusterManagerByNamespace(namespace, clustermanagers)
		if err != nil {
			return ""
		}

		return clustermanager.Name
	}
}

func ClusterManagerSecretQueueKeyFunc(clusterManagerLister operatorlister.ClusterManagerLister) factory.ObjectQueueKeyFunc {
	return clusterManagerByNamespaceQueueKeyFunc(clusterManagerLister)
}

func ClusterManagerConfigmapQueueKeyFunc(clusterManagerLister operatorlister.ClusterManagerLister) factory.ObjectQueueKeyFunc {
	return clusterManagerByNamespaceQueueKeyFunc(clusterManagerLister)
}

func clusterManagerByNamespaceQueueKeyFunc(clusterManagerLister operatorlister.ClusterManagerLister) factory.ObjectQueueKeyFunc {
	return func(obj runtime.Object) string {
		accessor, _ := meta.Accessor(obj)
		namespace := accessor.GetNamespace()

		clustermanagers, err := clusterManagerLister.List(labels.Everything())
		if err != nil {
			return ""
		}

		clustermanager, err := FindClusterManagerByNamespace(namespace, clustermanagers)
		if err != nil {
			return ""
		}

		return clustermanager.Name
	}
}

func FindKlusterletByNamespace(klusterlets []*operatorapiv1.Klusterlet, namespace string) *operatorapiv1.Klusterlet {
	for _, klusterlet := range klusterlets {
		agentNamespace := AgentNamespace(klusterlet)
		if namespace == agentNamespace {
			return klusterlet
		}
	}
	return nil
}

func FindClusterManagerByNamespace(namespace string, clusterManagers []*operatorapiv1.ClusterManager) (*operatorapiv1.ClusterManager, error) {
	for i := range clusterManagers {
		if clusterManagers[i].Name == namespace ||
			(clusterManagers[i].Spec.DeployOption.Mode == operatorapiv1.InstallModeDefault && namespace == ClusterManagerDefaultNamespace) {
			return clusterManagers[i], nil
		}
	}
	return nil, fmt.Errorf("no match for namespace %s", namespace)
}
