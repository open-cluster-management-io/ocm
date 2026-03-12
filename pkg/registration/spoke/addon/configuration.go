package addon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	addonwebhook "open-cluster-management.io/ocm/pkg/addon/webhook/v1beta1"
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
	addOnName string,
	installOption addonInstallOption,
	registrations []addonv1beta1.RegistrationConfig,
) (map[string]registrationConfig, error) {
	configs := map[string]registrationConfig{}

	for _, registration := range registrations {
		config := registrationConfig{
			addOnName:          addOnName,
			addonInstallOption: installOption,
			registration:       registration,
		}

		// set the secret name of client certificate
		switch registration.Type {
		case addonv1beta1.KubeClient:
			config.secretName = fmt.Sprintf("%s-hub-kubeconfig", addOnName)
		case addonv1beta1.CustomSigner:
			if registration.CustomSigner == nil || registration.CustomSigner.SignerName == "" {
				return configs, fmt.Errorf("custom signer registration for addon %q is missing signerName", addOnName)
			}
			config.secretName = fmt.Sprintf("%s-%s-client-cert", addOnName, strings.ReplaceAll(registration.CustomSigner.SignerName, "/", "-"))
		default:
			return configs, fmt.Errorf("unsupported registration type %q for addon %q", registration.Type, addOnName)
		}

		// hash registration configuration, install namespace and addOnAgentRunningOutsideManagedCluster. Use the hash
		// value as the key of map to make sure each registration configuration and addon installation option is unique
		hash, err := getConfigHash(
			registration,
			config.addonInstallOption)
		if err != nil {
			return configs, err
		}
		config.hash = hash
		configs[config.hash] = config
	}

	return configs, nil
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
