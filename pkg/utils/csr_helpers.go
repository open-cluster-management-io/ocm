package utils

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	openshiftcrypto "github.com/openshift/library-go/pkg/crypto"
	"github.com/pkg/errors"
	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
)

const (
	TLSCACert = "ca.crt"
	TLSCAKey  = "ca.key"
)

var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

// DefaultSignerWithExpiry generates a signer func for addon agent to sign the csr using caKey and caData with expiry date.
func DefaultSignerWithExpiry(caKey, caData []byte, duration time.Duration) agent.CSRSignerFunc {
	return func(csr *certificatesv1.CertificateSigningRequest) []byte {
		blockTlsCrt, _ := pem.Decode(caData) // note: the second return value is not error for pem.Decode; it's ok to omit it.
		certs, err := x509.ParseCertificates(blockTlsCrt.Bytes)
		if err != nil {
			klog.Errorf("Failed to parse cert: %v", err)
			return nil
		}

		blockTlsKey, _ := pem.Decode(caKey)

		// For now only PKCS#1 is supported which assures the private key algorithm is RSA.
		// TODO: Compatibility w/ PKCS#8 key e.g. EC algorithm
		key, err := x509.ParsePKCS1PrivateKey(blockTlsKey.Bytes)
		if err != nil {
			klog.Errorf("Failed to parse key: %v", err)
			return nil
		}

		data, err := signCSR(csr, certs[0], key, duration)
		if err != nil {
			klog.Errorf("Failed to sign csr: %v", err)
			return nil
		}
		return data
	}
}

func CustomSignerWithExpiry(
	kubeclient kubernetes.Interface,
	customSignerConfig *addonapiv1alpha1.CustomSignerRegistrationConfig,
	duration time.Duration) agent.CSRSignerFunc {
	return func(csr *certificatesv1.CertificateSigningRequest) []byte {
		if customSignerConfig == nil {
			utilruntime.HandleError(fmt.Errorf("custome signer is nil"))
			return nil
		}

		if csr.Spec.SignerName != customSignerConfig.SignerName {
			return nil
		}
		caSecret, err := kubeclient.CoreV1().Secrets(customSignerConfig.SigningCA.Namespace).Get(
			context.TODO(), customSignerConfig.SigningCA.Name, metav1.GetOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("get custome signer ca %s/%s failed, %v",
				customSignerConfig.SigningCA.Namespace, customSignerConfig.SigningCA.Name, err))
			return nil
		}

		caData, caKey, err := extractCAdata(caSecret.Data[TLSCACert], caSecret.Data[TLSCAKey])
		if customSignerConfig == nil {
			utilruntime.HandleError(fmt.Errorf("get ca %s/%s data failed, %v",
				customSignerConfig.SigningCA.Namespace, customSignerConfig.SigningCA.Name, err))
			return nil
		}
		return DefaultSignerWithExpiry(caKey, caData, duration)(csr)
	}
}

func extractCAdata(caCertData, caKeyData []byte) ([]byte, []byte, error) {
	certBlock, _ := pem.Decode(caCertData)
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to parse ca certificate")
	}
	keyBlock, _ := pem.Decode(caKeyData)
	caKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to parse ca key")
	}

	caConfig := &openshiftcrypto.TLSCertificateConfig{
		Certs: []*x509.Certificate{caCert},
		Key:   caKey,
	}
	return caConfig.GetPEMBytes()
}

func signCSR(csr *certificatesv1.CertificateSigningRequest, caCert *x509.Certificate, caKey *rsa.PrivateKey, duration time.Duration) ([]byte, error) {
	certExpiryDuration := duration
	durationUntilExpiry := time.Until(caCert.NotAfter)
	if durationUntilExpiry <= 0 {
		return nil, fmt.Errorf("signer has expired, expired time: %v", caCert.NotAfter)
	}
	if durationUntilExpiry < certExpiryDuration {
		certExpiryDuration = durationUntilExpiry
	}

	request, err := parseCSR(csr.Spec.Request)
	if err != nil {
		return nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("unable to generate a serial number for %s: %v", request.Subject.CommonName, err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:       serialNumber,
		Subject:            request.Subject,
		DNSNames:           request.DNSNames,
		IPAddresses:        request.IPAddresses,
		EmailAddresses:     request.EmailAddresses,
		URIs:               request.URIs,
		PublicKeyAlgorithm: request.PublicKeyAlgorithm,
		PublicKey:          request.PublicKey,
		Extensions:         request.Extensions,
		ExtraExtensions:    request.ExtraExtensions,
		// Hard code the usage since it cannot be specified in registration process
		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	now := time.Now()
	tmpl.NotBefore = now
	tmpl.NotAfter = now.Add(certExpiryDuration)

	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, request.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	}), nil
}

func parseCSR(pemBytes []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("PEM block type must be CERTIFICATE REQUEST")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	return csr, nil
}

// KubeClientCSRApprover approve the csr when addon agent uses default group, default user and
// "kubernetes.io/kube-apiserver-client" signer to sign csr.
func KubeClientCSRApprover(agentName string) agent.CSRApproveFunc {
	return func(
		cluster *clusterv1.ManagedCluster,
		addon *addonapiv1alpha1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {
		if csr.Spec.SignerName != certificatesv1.KubeAPIServerClientSignerName {
			return false
		}
		return DefaultCSRApprover(agentName)(cluster, addon, csr)
	}
}

// DefaultCSRApprover approve the csr when addon agent uses default group and default user to sign csr.
func DefaultCSRApprover(agentName string) agent.CSRApproveFunc {
	return func(
		cluster *clusterv1.ManagedCluster,
		addon *addonapiv1alpha1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {
		defaultGroups := agent.DefaultGroups(cluster.Name, addon.Name)

		defaultUser := agent.DefaultUser(cluster.Name, addon.Name, agentName)
		// check org field and commonName field
		block, _ := pem.Decode(csr.Spec.Request)
		if block == nil || block.Type != "CERTIFICATE REQUEST" {
			klog.Infof("CSR Approve Check Failed csr %q was not recognized: PEM block type is not CERTIFICATE REQUEST", csr.Name)
			return false
		}

		x509cr, err := x509.ParseCertificateRequest(block.Bytes)
		if err != nil {
			klog.Infof("CSR Approve Check Failed csr %q was not recognized: %v", csr.Name, err)
			return false
		}

		requestingOrgs := sets.NewString(x509cr.Subject.Organization...)
		if requestingOrgs.Len() != 3 {
			klog.Infof("CSR Approve Check Failed csr %q org is not equal to 3", csr.Name)
			return false
		}

		for _, group := range defaultGroups {
			if !requestingOrgs.Has(group) {
				klog.Infof("CSR Approve Check Failed csr requesting orgs doesn't contain %s", group)
				return false
			}
		}

		// check commonName field
		if defaultUser != x509cr.Subject.CommonName {
			klog.Infof("CSR Approve Check Failed commonName not right; request %s get %s", x509cr.Subject.CommonName, defaultUser)
			return false
		}

		// check user name
		if strings.HasPrefix(csr.Spec.Username, "system:open-cluster-management:"+cluster.Name) {
			klog.Info("CSR approved")
			return true
		} else {
			klog.Info("CSR not approved due to illegal requester", "requester", csr.Spec.Username)
			return false
		}
	}
}

// CustomerSignerCSRApprover approve the csr when addon agent uses custom signer to sign csr.
func CustomerSignerCSRApprover(agentName string) agent.CSRApproveFunc {
	return func(
		cluster *clusterv1.ManagedCluster,
		addon *addonapiv1alpha1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {

		klog.Infof("Customer signer CSR is approved. cluster: %s, addon %s, requester: %s",
			cluster.Name, addon.Name, csr.Spec.Username)
		return true
	}
}

// IsCSRSupported checks whether the cluster supports v1 or v1beta1 csr api.
func IsCSRSupported(nativeClient kubernetes.Interface) (bool, bool, error) {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(nativeClient.Discovery()))
	mappings, err := mapper.RESTMappings(schema.GroupKind{
		Group: certificatesv1.GroupName,
		Kind:  "CertificateSigningRequest",
	})
	if err != nil {
		return false, false, err
	}
	v1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1" {
			v1CSRSupported = true
		}
	}
	v1beta1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1beta1" {
			v1beta1CSRSupported = true
		}
	}
	return v1CSRSupported, v1beta1CSRSupported, nil
}

func TemplateCSRConfigurationsFunc(
	addonName, agentName string,
	addonClient addonv1alpha1client.Interface,
) func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {

	return func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
		addon, err := addonClient.AddonV1alpha1().ManagedClusterAddOns(cluster.Name).Get(
			context.TODO(), addonName, metav1.GetOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon %s: %v", addonName, err))
			return nil
		}
		template, err := GetDesiredAddOnTemplate(addonClient, addon)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon %s template: %v", addonName, err))
			return nil
		}
		if template == nil {
			return nil
		}

		contain := func(rcs []addonapiv1alpha1.RegistrationConfig, signerName string) bool {
			for _, rc := range rcs {
				if rc.SignerName == signerName {
					return true
				}
			}
			return false
		}

		registrationConfigs := make([]addonapiv1alpha1.RegistrationConfig, 0)
		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:
				if !contain(registrationConfigs, certificatesv1.KubeAPIServerClientSignerName) {
					configs := agent.KubeClientSignerConfigurations(addonName, agentName)(cluster)
					registrationConfigs = append(registrationConfigs, configs...)
				}

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				if registration.CustomSigner == nil {
					continue
				}
				if !contain(registrationConfigs, registration.CustomSigner.SignerName) {
					configs := agent.CustomSignerConfigurations(
						addonName, agentName, registration.CustomSigner)(cluster)
					registrationConfigs = append(registrationConfigs, configs...)
				}

			default:
				utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
			}

		}

		return registrationConfigs
	}
}

func TemplateCSRApproveCheckFunc(
	addonName, agentName string,
	addonClient addonv1alpha1client.Interface,
) agent.CSRApproveFunc {

	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {

		template, err := GetDesiredAddOnTemplate(addonClient, addon)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon %s template: %v", addonName, err))
			return false
		}
		if template == nil {
			return false
		}

		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:

				if csr.Spec.SignerName == certificatesv1.KubeAPIServerClientSignerName {
					return KubeClientCSRApprover(agentName)(cluster, addon, csr)
				}

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				if registration.CustomSigner == nil {
					continue
				}
				if csr.Spec.SignerName == registration.CustomSigner.SignerName {
					return CustomerSignerCSRApprover(addonName)(cluster, addon, csr)
				}

			default:
				utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
			}

		}

		return false
	}
}

func TemplateCSRSignFunc(
	addonName, agentName string,
	addonClient addonv1alpha1client.Interface,
	hubKubeClient kubernetes.Interface,
) agent.CSRSignerFunc {

	return func(csr *certificatesv1.CertificateSigningRequest) []byte {
		// TODO: consider to change the agent.CSRSignerFun to accept parameter addon
		getClusterName := func(userName string) string {
			// the common name of csr is in format of "system:open-cluster-management:{clusterName}:{id}"
			// get the cluster name from the common name
			trimedCommonName := strings.TrimLeft(userName, "system:open-cluster-management:")
			return strings.Split(trimedCommonName, ":")[0]
		}

		clusterName := getClusterName(csr.Spec.Username)
		addon, err := addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
			context.TODO(), addonName, metav1.GetOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon %s: %v", addonName, err))
			return nil
		}

		template, err := GetDesiredAddOnTemplate(addonClient, addon)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon %s template: %v", addonName, err))
			return nil
		}
		if template == nil {
			return nil
		}

		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:
				continue

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				if registration.CustomSigner == nil {
					continue
				}
				if csr.Spec.SignerName == registration.CustomSigner.SignerName {
					return CustomSignerWithExpiry(hubKubeClient, registration.CustomSigner, 24*time.Hour)(csr)
				}

			default:
				utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
			}

		}

		return nil
	}
}

// GetDesiredAddOnTemplate returns the desired template of the addon
func GetDesiredAddOnTemplate(
	addonClient addonv1alpha1client.Interface,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.AddOnTemplate, error) {
	ok, templateRef := AddonTemplateConfigRef(addon.Status.ConfigReferences)
	if !ok {
		klog.V(4).Infof("Addon %s template config in status is empty", addon.Name)
		return nil, nil
	}

	desiredTemplate := templateRef.DesiredConfig
	if desiredTemplate == nil || desiredTemplate.SpecHash == "" {
		klog.Infof("Addon %s template spec hash is empty", addon.Name)
		return nil, fmt.Errorf("addon %s template desired spec hash is empty", addon.Name)
	}

	template, err := addonClient.AddonV1alpha1().AddOnTemplates().Get(
		context.TODO(), desiredTemplate.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return template, nil
}
