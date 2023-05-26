package addon

import (
	"context"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"strings"

	certificatesv1 "k8s.io/api/certificates/v1"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	defaultAddOnInstallationNamespace = "open-cluster-management-agent-addon"
	// hostingClusterNameAnnotation is the annotation for indicating the hosting cluster name
	hostingClusterNameAnnotation = "addon.open-cluster-management.io/hosting-cluster-name"
)

// registrationConfig contains necessary information for addon registration
// TODO: Refactor the code here once the registration configuration is available in spec of ManagedClusterAddOn
type registrationConfig struct {
	addOnName    string
	registration addonv1alpha1.RegistrationConfig

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

func (c *registrationConfig) x509Subject(clusterName, agentName string) *pkix.Name {
	subject := &pkix.Name{
		CommonName:         c.registration.Subject.User,
		Organization:       c.registration.Subject.Groups,
		OrganizationalUnit: c.registration.Subject.OrganizationUnits,
	}

	// set the default common name
	if len(subject.CommonName) == 0 {
		subject.CommonName = defaultCommonName(clusterName, agentName, c.addOnName)
	}

	// set the default organization if signer is KubeAPIServerClientSignerName
	if c.registration.SignerName == certificatesv1.KubeAPIServerClientSignerName && len(subject.Organization) == 0 {
		subject.Organization = []string{defaultOrganization(clusterName, c.addOnName)}
	}

	return subject
}

// getAddOnInstallationNamespace returns addon installation namespace from addon spec.
// It first checks the installation namespace in status then addon spec, the addon default
// installation namespace open-cluster-management-agent-addon will be returned.
func getAddOnInstallationNamespace(addOn *addonv1alpha1.ManagedClusterAddOn) string {
	installationNamespace := addOn.Status.Namespace
	if installationNamespace == "" {
		installationNamespace = addOn.Spec.InstallNamespace
	}
	if installationNamespace == "" {
		installationNamespace = defaultAddOnInstallationNamespace
	}

	return installationNamespace
}

// isAddonRunningOutsideManagedCluster returns whether the addon agent is running on the managed cluster
func isAddonRunningOutsideManagedCluster(addOn *addonv1alpha1.ManagedClusterAddOn) bool {
	hostingCluster, ok := addOn.Annotations[hostingClusterNameAnnotation]
	if ok && len(hostingCluster) != 0 {
		return true
	}
	return false
}

// getRegistrationConfigs reads annotations of a addon and returns a map of registrationConfig whose
// key is the hash of the registrationConfig
func getRegistrationConfigs(addOn *addonv1alpha1.ManagedClusterAddOn) (map[string]registrationConfig, error) {
	configs := map[string]registrationConfig{}

	for _, registration := range addOn.Status.Registrations {
		config := registrationConfig{
			addOnName: addOn.Name,
			addonInstallOption: addonInstallOption{
				AgentRunningOutsideManagedCluster: isAddonRunningOutsideManagedCluster(addOn),
				InstallationNamespace:             getAddOnInstallationNamespace(addOn),
			},
			registration: registration,
		}

		// set the secret name of client certificate
		switch registration.SignerName {
		case certificatesv1.KubeAPIServerClientSignerName:
			config.secretName = fmt.Sprintf("%s-hub-kubeconfig", addOn.Name)
		default:
			config.secretName = fmt.Sprintf("%s-%s-client-cert", addOn.Name, strings.ReplaceAll(registration.SignerName, "/", "-"))
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

func getConfigHash(registration addonv1alpha1.RegistrationConfig, installOption addonInstallOption) (string, error) {
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

func defaultCommonName(clusterName, agentName, addonName string) string {
	return fmt.Sprintf("%s:agent:%s", defaultOrganization(clusterName, addonName), agentName)
}

func defaultOrganization(clusterName, addonName string) string {
	return fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s", clusterName, addonName)
}
