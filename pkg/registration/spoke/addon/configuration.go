package addon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/klog/v2"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	addonwebhook "open-cluster-management.io/ocm/pkg/addon/webhook/v1beta1"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
)

const (
	defaultAddOnInstallationNamespace = "open-cluster-management-agent-addon"
)

// registrationConfig contains necessary information for addon registration
type registrationConfig struct {
	addOnName    string
	registration addonv1beta1.RegistrationConfig

	// secretName is the name of secret containing client certificate. If the SignerName is "kubernetes.io/kube-apiserver-client",
	// the secret name will be "{addon name}-hub-kubeconfig". Otherwise, the secret name will be "{addon name}-{signer name}-client-cert".
	secretName string
	hash       string
	stopFunc   context.CancelFunc

	addonInstallOption
}

type addonInstallOption struct {
	InstallationNamespace             string `json:"installationNamespace"`
	AgentRunningOutsideManagedCluster bool   `json:"agentRunningOutsideManagedCluster"`
}

// getAddOnInstallationNamespace returns addon installation namespace from addon spec.
// It first checks the installation namespace in status then addon spec, the addon default
// installation namespace open-cluster-management-agent-addon will be returned.
func getAddOnInstallationNamespace(addOn *addonv1beta1.ManagedClusterAddOn) string {
	installationNamespace := addOn.Status.Namespace
	if installationNamespace == "" {
		annotation, ok := addOn.Annotations[addonwebhook.InstallNamespaceAnnotation]
		if ok {
			installationNamespace = annotation
		}
	}
	if installationNamespace == "" {
		installationNamespace = defaultAddOnInstallationNamespace
	}

	return installationNamespace
}

// isAddonRunningOutsideManagedCluster returns whether the addon agent is running on the managed cluster
func isAddonRunningOutsideManagedCluster(addOn *addonv1beta1.ManagedClusterAddOn) bool {
	hostingCluster, ok := addOn.Annotations[addonv1beta1.HostingClusterNameAnnotationKey]
	if ok && len(hostingCluster) != 0 {
		return true
	}
	return false
}

// getRegistrationConfigs reads registrations and returns a map of registrationConfig whose
// key is the hash of the registrationConfig.
func getRegistrationConfigs(
	addOnName, clusterName string,
	installOption addonInstallOption,
	registrations []addonv1beta1.RegistrationConfig,
	logger klog.Logger,
) (map[string]registrationConfig, error) {
	configs := map[string]registrationConfig{}

	for _, registration := range registrations {
		config := registrationConfig{
			addOnName:          addOnName,
			addonInstallOption: installOption,
		}

		// set the secret name of client certificate and subject if it is not set from the registration
		switch registration.Type {
		case addonv1beta1.KubeClient:
			if registration.KubeClient == nil {
				logger.Info("kube client is not configured")
				continue
			}
			config.secretName = fmt.Sprintf("%s-hub-kubeconfig", addOnName)
		case addonv1beta1.CustomSigner:
			// customer signer should have user and signer set before starting registration.
			if registration.CustomSigner == nil || registration.CustomSigner.SignerName == "" {
				logger.Info("customer signer is not configured")
				continue
			}
			config.secretName = fmt.Sprintf("%s-%s-client-cert", addOnName, strings.ReplaceAll(registration.CustomSigner.SignerName, "/", "-"))
		default:
			logger.Info("unsupported registration type", "type", registration.Type)
			continue
		}
		config.registration = setSubjectForRegistration(addOnName, clusterName, registration)

		// hash registration configuration, install namespace and addOnAgentRunningOutsideManagedCluster. Use the hash
		// value as the key of map to make sure each registration configuration and addon installation option is unique
		hash, err := getConfigHash(
			config.registration,
			config.addonInstallOption)
		if err != nil {
			return configs, err
		}
		config.hash = hash
		configs[config.hash] = config
	}

	return configs, nil
}

// setSubjectForRegistration is mainly to set subject of registration when it is not set.
// This is to support the backward compatibility with old version of addon-framework when user/groups is not set.
func setSubjectForRegistration(addonName, clusterName string, registration addonv1beta1.RegistrationConfig) addonv1beta1.RegistrationConfig {
	registrationCopy := registration.DeepCopy()

	switch registration.Type {
	case addonv1beta1.KubeClient:
		if registration.KubeClient == nil {
			return *registrationCopy
		}
		switch registration.KubeClient.Driver {
		case "csr":
			if registrationCopy.KubeClient.Subject.User == "" {
				registrationCopy.KubeClient.Subject.User = csr.DefaultCommonName(clusterName, addonName)
			}
			if len(registrationCopy.KubeClient.Subject.Groups) == 0 {
				registrationCopy.KubeClient.Subject.Groups = []string{csr.DefaultOrganization(clusterName, addonName)}
			}
		case "token":
			registrationCopy.KubeClient.Subject = token.TokenSubject(clusterName, addonName)
		}
	case addonv1beta1.CustomSigner:
		if registrationCopy.CustomSigner == nil {
			return *registrationCopy
		}
		if registrationCopy.CustomSigner.Subject.User == "" {
			registrationCopy.CustomSigner.Subject.User = csr.DefaultCommonName(clusterName, addonName)
		}
	}

	return *registrationCopy
}

func getConfigHash(registration addonv1beta1.RegistrationConfig, installOption addonInstallOption) (string, error) {
	data, err := json.Marshal(registration)
	if err != nil {
		return "", err
	}

	installOptionData, err := json.Marshal(installOption)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write(data)
	h.Write(installOptionData)

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
