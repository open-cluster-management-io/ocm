package addon

import (
	"context"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"strings"

	addonv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
)

const (
	defaultAddOnInstallationNamespace = "open-cluster-management-agent-addon"
	installationNamespaceAnnotation   = "addon.open-cluster-management.io/installNamespace"
	// registrationAnnotation includes the addOn registration configuration. If specified, it means the registration is enabled.
	// The value is a json string, like
	// `[ { "signerName":"signer1", "subject":{ "commonName": "addon1-agent", "organization":["addon1"], "organizationalUnit":["acm"] }]`
	// where subject is optional.
	registrationAnnotation = "addon.open-cluster-management.io/registrations"
)

// addonConfig contains addon configuration information.
type addOnConfig struct {
	AddOnName             string
	InstallationNamespace string
}

// registrationConfig contains necessary information for addon registration
// TODO: Refactor the code here once the registration configuration is available in spec of ManagedClusterAddOn
type registrationConfig struct {
	AddOnName             string `json:"addOnName,omitempty"`
	InstallationNamespace string `json:"installNamespace,omitempty"`
	// SignerName is used to create csr. The default value is "kubernetes.io/kube-apiserver-client".
	SignerName string      `json:"signerName,omitempty"`
	Subject    certSubject `json:"subject,omitempty"`

	// secretName is the name of secret containing client certificate. If the SignerName is "kubernetes.io/kube-apiserver-client",
	// the secret name will be "{addon name}-hub-kubeconfig". Otherwise, the secret name will be "{addon name}-{signer name}-client-cert".
	secretName string
	hash       string
	stopFunc   context.CancelFunc
}

func (c *registrationConfig) x509Subject(clusterName, agentName string) *pkix.Name {
	subject := &pkix.Name{
		CommonName:         c.Subject.CommonName,
		Organization:       c.Subject.Organization,
		OrganizationalUnit: c.Subject.OrganizationalUnit,
	}

	// set the default common name
	if len(subject.CommonName) == 0 {
		subject.CommonName = defaultCommonName(clusterName, agentName, c.AddOnName)
	}

	// set the default organization if signer is KubeAPIServerClientSignerName
	if c.SignerName == certificatesv1beta1.KubeAPIServerClientSignerName && len(subject.Organization) == 0 {
		subject.Organization = []string{defaultOrganization(clusterName, c.AddOnName)}
	}

	return subject
}

// certSubject contains fields which are used to generate csr subject and can be customized by users
type certSubject struct {
	CommonName         string   `json:"commonName,omitempty"`
	Organization       []string `json:"organization,omitempty"`
	OrganizationalUnit []string `json:"organizationalUnit,omitempty"`
}

// getAddOnConfig returns addon configuration information from addon annotations.
func getAddOnConfig(addOn *addonv1alpha1.ManagedClusterAddOn) (*addOnConfig, error) {
	installationNamespace, ok := addOn.Annotations[installationNamespaceAnnotation]
	if !ok {
		installationNamespace = defaultAddOnInstallationNamespace
	}

	addOnConfig := &addOnConfig{
		AddOnName:             addOn.Name,
		InstallationNamespace: installationNamespace,
	}

	return addOnConfig, nil
}

// getRegistrationConfigs reads annotations of a addon and returns a map of registrationConfig whose
// key is the hash of the registrationConfig
func getRegistrationConfigs(addOnName string, annotations map[string]string) (map[string]registrationConfig, error) {
	ns := annotations[installationNamespaceAnnotation]
	if len(ns) == 0 {
		return nil, nil
	}

	registration := annotations[registrationAnnotation]
	if len(registration) == 0 {
		return nil, nil
	}

	// parse registration configs from annotation
	items := []registrationConfig{}
	err := json.Unmarshal([]byte(registration), &items)
	if err != nil {
		return nil, err
	}

	configs := map[string]registrationConfig{}
	for _, item := range items {
		item.AddOnName = addOnName
		item.InstallationNamespace = ns

		// set the default signer name
		if len(item.SignerName) == 0 {
			item.SignerName = certificatesv1beta1.KubeAPIServerClientSignerName
		}
		// set the secret name of client certificate
		switch item.SignerName {
		case certificatesv1beta1.KubeAPIServerClientSignerName:
			item.secretName = fmt.Sprintf("%s-hub-kubeconfig", addOnName)
		default:
			item.secretName = fmt.Sprintf("%s-%s-client-cert", addOnName, strings.ReplaceAll(item.SignerName, "/", "-"))
		}

		data, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		h.Write(data)
		item.hash = fmt.Sprintf("%x", h.Sum(nil))
		configs[item.hash] = item
	}

	return configs, nil
}

func defaultCommonName(clusterName, agentName, addonName string) string {
	return fmt.Sprintf("%s:agent:%s", defaultOrganization(clusterName, addonName), agentName)
}

func defaultOrganization(clusterName, addonName string) string {
	return fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s", clusterName, addonName)
}
