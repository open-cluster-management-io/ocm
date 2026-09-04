package addon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/klog/v2"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	addonwebhook "open-cluster-management.io/ocm/pkg/addon/webhook/v1beta1"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
)

const (
	defaultAddOnInstallationNamespace = "open-cluster-management-agent-addon"

	// ManagedCluster annotations that gate hosted-mode addon deployment (aligned with
	// multicloud-operators-foundation HostedClusterInfo and klusterlet-addon-controller).
	annotationEnableHostedModeAddons       = "addon.open-cluster-management.io/enable-hosted-mode-addons"
	annotationKlusterletDeployMode         = "import.open-cluster-management.io/klusterlet-deploy-mode"
	annotationKlusterletHostingClusterName = "import.open-cluster-management.io/hosting-cluster-name"
	klusterletDeployModeHosted             = "Hosted"
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

// isAddonRunningOutsideManagedCluster returns whether the addon agent is running outside the managed cluster
// (hosted mode). Signals are evaluated in order:
//  1. ManagedClusterAddOn addon.open-cluster-management.io/hosting-cluster-name annotation
//  2. Install namespace klusterlet-{managed cluster name} (observed per-addon deployment location)
func isAddonRunningOutsideManagedCluster(
	addOn *addonv1beta1.ManagedClusterAddOn,
	managedCluster *clusterv1.ManagedCluster,
	logger klog.Logger,
) bool {
	addonName, clusterName := addonNamespacedName(addOn)
	installNamespace := getAddOnInstallationNamespace(addOn)

	if hasAddonHostingClusterNameAnnotation(addOn.GetAnnotations()) {
		logHostedModeDecision(logger, addonName, clusterName, true,
			"managedClusterAddOn hosting-cluster-name annotation",
			"hostingCluster", addOn.Annotations[addonv1beta1.HostingClusterNameAnnotationKey],
			"installNamespace", installNamespace)
		return true
	}
	if isAddonInstalledInKlusterletNamespace(addOn) {
		logHostedModeDecision(logger, addonName, clusterName, true,
			"klusterlet install namespace fallback",
			"installNamespace", installNamespace,
			"expectedNamespace", fmt.Sprintf("klusterlet-%s", addOn.Namespace))
		return true
	}
	logHostedModeDecision(logger, addonName, clusterName, false,
		"addon runs on managed cluster",
		"installNamespace", installNamespace)
	return false
}

// isAddonInstalledInKlusterletNamespace reports whether this addon is deployed into the hosted-mode
// namespace on the management cluster (klusterlet-{managed cluster name}).
func isAddonInstalledInKlusterletNamespace(addOn *addonv1beta1.ManagedClusterAddOn) bool {
	if addOn == nil {
		return false
	}
	installNamespace := getAddOnInstallationNamespace(addOn)
	return installNamespace == fmt.Sprintf("klusterlet-%s", addOn.Namespace)
}

func hasAddonHostingClusterNameAnnotation(annotations map[string]string) bool {
	if len(annotations) == 0 {
		return false
	}
	hostingCluster, ok := annotations[addonv1beta1.HostingClusterNameAnnotationKey]
	return ok && len(hostingCluster) != 0
}

func managedClusterEnablesHostedAddons(cluster *clusterv1.ManagedCluster) bool {
	if cluster == nil || len(cluster.Annotations) == 0 {
		return false
	}
	annotations := cluster.Annotations
	if !strings.EqualFold(annotations[annotationEnableHostedModeAddons], "true") {
		return false
	}
	if annotations[annotationKlusterletDeployMode] != klusterletDeployModeHosted {
		return false
	}
	return len(annotations[annotationKlusterletHostingClusterName]) > 0
}

// hostingClusterNameForAddon returns the hosting cluster name from the addon annotation, or from the
// ManagedCluster import hosting-cluster-name annotation when the addon runs in hosted mode via fallback.
func hostingClusterNameForAddon(
	addOn *addonv1beta1.ManagedClusterAddOn,
	managedCluster *clusterv1.ManagedCluster,
	logger klog.Logger,
) string {
	addonName, clusterName := addonNamespacedName(addOn)

	if hasAddonHostingClusterNameAnnotation(addOn.GetAnnotations()) {
		return addOn.Annotations[addonv1beta1.HostingClusterNameAnnotationKey]
	}
	if isAddonInstalledInKlusterletNamespace(addOn) && managedClusterEnablesHostedAddons(managedCluster) {
		hostingCluster := managedCluster.Annotations[annotationKlusterletHostingClusterName]
		logHostedModeDecision(logger, addonName, clusterName, true,
			"managedCluster import hosting-cluster-name fallback",
			"hostingCluster", hostingCluster,
			"installNamespace", getAddOnInstallationNamespace(addOn))
		return hostingCluster
	}
	return ""
}

func addonNamespacedName(addOn *addonv1beta1.ManagedClusterAddOn) (name, namespace string) {
	if addOn == nil {
		return "", ""
	}
	return addOn.Name, addOn.Namespace
}

func logHostedModeDecision(logger klog.Logger, addonName, clusterName string, outsideManagedCluster bool, reason string, keysAndValues ...interface{}) {
	if !logger.Enabled() {
		return
	}
	args := []interface{}{
		"addon", addonName,
		"cluster", clusterName,
		"outsideManagedCluster", outsideManagedCluster,
		"reason", reason,
	}
	args = append(args, keysAndValues...)
	if outsideManagedCluster {
		logger.Info("determined addon hosted mode", args...)
		return
	}
	logger.V(4).Info("determined addon hosted mode", args...)
}

func getManagedClusterFromLister(lister clusterv1listers.ManagedClusterLister, clusterName string) *clusterv1.ManagedCluster {
	if lister == nil {
		return nil
	}
	cluster, err := lister.Get(clusterName)
	if err != nil {
		return nil
	}
	return cluster
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
