package helpers

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/library-go/pkg/controller/factory"

	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

const (
	// KlusterletDefaultNamespace is the default namespace of klusterlet
	KlusterletDefaultNamespace = "open-cluster-management-agent"
	// BootstrapHubKubeConfig is the secret name of bootstrap kubeconfig secret to connect to hub
	BootstrapHubKubeConfig = "bootstrap-hub-kubeconfig"
	// HubKubeConfig is the secret name of kubeconfig secret to connect to hub with mtls
	HubKubeConfig = "hub-kubeconfig-secret"
	// ExternalManagedKubeConfig is the secret name of kubeconfig secret to connecting to the managed cluster
	// Only applicable to Detached mode, klusterlet-operator uses it to install resources on the managed cluster.
	ExternalManagedKubeConfig = "external-managed-kubeconfig"
	// ExternalManagedKubeConfigRegistration is the secret name of kubeconfig secret to connecting to the managed cluster
	// Only applicable to Detached mode, registration-agent uses it to connect to the managed cluster.
	ExternalManagedKubeConfigRegistration = "external-managed-kubeconfig-registration"
	// ExternalManagedKubeConfigWork is the secret name of kubeconfig secret to connecting to the managed cluster
	// Only applicable to Detached mode, work-agent uses it to connect to the managed cluster.
	ExternalManagedKubeConfigWork = "external-managed-kubeconfig-work"
	// ClusterManagerNamespace is the namespace to deploy cluster manager components
	ClusterManagerNamespace = "open-cluster-management-hub"

	RegistrationWebhookSecret  = "registration-webhook-serving-cert"
	RegistrationWebhookService = "cluster-manager-registration-webhook"
	WorkWebhookSecret          = "work-webhook-serving-cert"
	WorkWebhookService         = "cluster-manager-work-webhook"
)

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
		namespace := accessor.GetNamespace()
		name := accessor.GetName()
		interestedObjectFound := false
		if namespace != ClusterManagerNamespace {
			return ""
		}
		if strings.HasSuffix(name, "registration-controller") || strings.HasSuffix(name, "work-controller") || strings.HasSuffix(name, "placement-controller") {
			interestedObjectFound = true
		}
		if !interestedObjectFound {
			return ""
		}

		clustermanagers, err := clusterManagerLister.List(labels.Everything())
		if err != nil {
			return ""
		}

		for _, clustermanager := range clustermanagers {
			return clustermanager.Name
		}

		return ""
	}
}

func ClusterManagerConfigmapQueueKeyFunc(clusterManagerLister operatorlister.ClusterManagerLister) factory.ObjectQueueKeyFunc {
	return func(obj runtime.Object) string {
		clustermanagers, err := clusterManagerLister.List(labels.Everything())
		if err != nil {
			return ""
		}

		for _, clustermanager := range clustermanagers {
			return clustermanager.Name
		}

		return ""
	}
}

func FindKlusterletByNamespace(klusterlets []*operatorapiv1.Klusterlet, namespace string) *operatorapiv1.Klusterlet {
	for _, klusterlet := range klusterlets {
		klusterletNS := KlusterletNamespace(klusterlet.Spec.DeployOption.Mode, klusterlet.Name, klusterlet.Spec.Namespace)
		if namespace == klusterletNS {
			return klusterlet
		}
	}
	return nil
}
