package templateagent

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	openshiftcrypto "github.com/openshift/library-go/pkg/crypto"
	"github.com/pkg/errors"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	// AddonTemplateLabelKey is the label key to set addon template name. It is to set the resources on the hub relating
	// to an addon template
	AddonTemplateLabelKey = "open-cluster-management.io/addon-template-name"
)

var (
	podNamespace = ""
)

func AddonManagerNamespace() string {
	if len(podNamespace) != 0 {
		return podNamespace
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if len(namespace) != 0 {
		podNamespace = namespace
	} else {
		podNamespace = "open-cluster-management-hub"
	}
	return podNamespace
}

func (a *CRDTemplateAgentAddon) GetDesiredAddOnTemplate(addon *addonapiv1alpha1.ManagedClusterAddOn,
	clusterName, addonName string) (*addonapiv1alpha1.AddOnTemplate, error) {
	if addon != nil {
		return a.getDesiredAddOnTemplateInner(addon.Name, addon.Status.ConfigReferences)
	}

	if len(clusterName) != 0 {
		addon, err := a.addonLister.ManagedClusterAddOns(clusterName).Get(addonName)
		if err != nil {
			return nil, err
		}

		return a.getDesiredAddOnTemplateInner(addon.Name, addon.Status.ConfigReferences)
	}

	// clusterName and addon are both empty, backoff to get the template from the clusterManagementAddOn
	cma, err := a.cmaLister.Get(addonName)
	if err != nil {
		return nil, err
	}

	// convert the DefaultConfigReference to ConfigReference
	var configReferences []addonapiv1alpha1.ConfigReference
	for _, configReference := range cma.Status.DefaultConfigReferences {
		configReferences = append(configReferences, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: configReference.ConfigGroupResource,
			DesiredConfig:       configReference.DesiredConfig,
		})
	}
	return a.getDesiredAddOnTemplateInner(cma.Name, configReferences)
}

func (a *CRDTemplateAgentAddon) TemplateCSRConfigurationsFunc() func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {

	return func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
		template, err := a.GetDesiredAddOnTemplate(nil, cluster.Name, a.addonName)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon %s template: %v", a.addonName, err))
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
					configs := agent.KubeClientSignerConfigurations(a.addonName, a.agentName)(cluster)
					registrationConfigs = append(registrationConfigs, configs...)
				}

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				if registration.CustomSigner == nil {
					continue
				}
				if !contain(registrationConfigs, registration.CustomSigner.SignerName) {
					configs := CustomSignerConfigurations(
						a.addonName, a.agentName, registration.CustomSigner)(cluster)
					registrationConfigs = append(registrationConfigs, configs...)
				}

			default:
				utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
			}

		}

		return registrationConfigs
	}
}

// CustomSignerConfigurations returns a func that can generate RegistrationConfig
// for CustomSigner type registration addon
func CustomSignerConfigurations(addonName, agentName string,
	customSignerConfig *addonapiv1alpha1.CustomSignerRegistrationConfig,
) func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
	return func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
		if customSignerConfig == nil {
			utilruntime.HandleError(fmt.Errorf("custome signer is nil"))
		}
		config := addonapiv1alpha1.RegistrationConfig{
			SignerName: customSignerConfig.SignerName,
			// TODO: confirm the subject
			Subject: addonapiv1alpha1.Subject{
				User:   agent.DefaultUser(cluster.Name, addonName, agentName),
				Groups: agent.DefaultGroups(cluster.Name, addonName),
			},
		}
		if customSignerConfig.Subject != nil {
			config.Subject = *customSignerConfig.Subject
		}

		return []addonapiv1alpha1.RegistrationConfig{config}
	}
}

func (a *CRDTemplateAgentAddon) TemplateCSRApproveCheckFunc() agent.CSRApproveFunc {

	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {

		template, err := a.GetDesiredAddOnTemplate(addon, cluster.Name, a.addonName)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon %s template: %v", a.addonName, err))
			return false
		}
		if template == nil {
			return false
		}

		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:

				if csr.Spec.SignerName == certificatesv1.KubeAPIServerClientSignerName {
					return KubeClientCSRApprover(a.agentName)(cluster, addon, csr)
				}

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				if registration.CustomSigner == nil {
					continue
				}
				if csr.Spec.SignerName == registration.CustomSigner.SignerName {
					return CustomerSignerCSRApprover(a.logger, a.addonName)(cluster, addon, csr)
				}

			default:
				utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
			}

		}

		return false
	}
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
		return utils.DefaultCSRApprover(agentName)(cluster, addon, csr)
	}
}

// CustomerSignerCSRApprover approve the csr when addon agent uses custom signer to sign csr.
func CustomerSignerCSRApprover(logger klog.Logger, agentName string) agent.CSRApproveFunc {
	return func(
		cluster *clusterv1.ManagedCluster,
		addon *addonapiv1alpha1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {

		logger.Info("Customer signer CSR is approved",
			"clusterName", cluster.Name,
			"addonName", addon.Name,
			"requester", csr.Spec.Username)
		return true
	}
}

func (a *CRDTemplateAgentAddon) TemplateCSRSignFunc() agent.CSRSignerFunc {

	return func(csr *certificatesv1.CertificateSigningRequest) []byte {
		// TODO: consider to change the agent.CSRSignerFun to accept parameter addon
		getClusterName := func(userName string) string {
			return csr.Labels[clusterv1.ClusterNameLabelKey]
		}

		clusterName := getClusterName(csr.Spec.Username)
		template, err := a.GetDesiredAddOnTemplate(nil, clusterName, a.addonName)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get template for addon %s in cluster %s: %v",
				a.addonName, clusterName, err))
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
					return CustomSignerWithExpiry(a.hubKubeClient, registration.CustomSigner, 24*time.Hour)(csr)
				}

			default:
				utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
			}

		}

		return nil
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
		caSecret, err := kubeclient.CoreV1().Secrets(AddonManagerNamespace()).Get(
			context.TODO(), customSignerConfig.SigningCA.Name, metav1.GetOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("get custome signer ca %s/%s failed, %v",
				AddonManagerNamespace(), customSignerConfig.SigningCA.Name, err))
			return nil
		}

		caData, caKey, err := extractCAdata(caSecret.Data[corev1.TLSCertKey], caSecret.Data[corev1.TLSPrivateKeyKey])
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("get ca %s/%s data failed, %v",
				AddonManagerNamespace(), customSignerConfig.SigningCA.Name, err))
			return nil
		}
		return utils.DefaultSignerWithExpiry(caKey, caData, duration)(csr)
	}
}

func extractCAdata(caCertData, caKeyData []byte) ([]byte, []byte, error) {
	certBlock, _ := pem.Decode(caCertData)
	if certBlock == nil {
		return nil, nil, errors.New("failed to decode ca cert")
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to parse ca certificate")
	}
	keyBlock, _ := pem.Decode(caKeyData)
	if keyBlock == nil {
		return nil, nil, errors.New("failed to decode ca key")
	}
	var errPkcs8, errPkcs1 error
	var caKey any
	caKey, errPkcs8 = x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if errPkcs8 != nil {
		caKey, errPkcs1 = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse ca key with pkcs8: %v and pkcs1: %v", errPkcs8, errPkcs1)
		}
	}

	caConfig := &openshiftcrypto.TLSCertificateConfig{
		Certs: []*x509.Certificate{caCert},
		Key:   caKey,
	}
	return caConfig.GetPEMBytes()
}

// TemplatePermissionConfigFunc returns a func that can grant permission for addon agent
// that is deployed by addon template.
// the returned func will create a rolebinding to bind the clusterRole/role which is
// specified by the user, so the user is required to make sure the existence of the
// clusterRole/role
func (a *CRDTemplateAgentAddon) TemplatePermissionConfigFunc() agent.PermissionConfigFunc {

	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		template, err := a.GetDesiredAddOnTemplate(addon, cluster.Name, a.addonName)
		if err != nil {
			return err
		}
		if template == nil {
			return nil
		}

		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:
				kcrc := registration.KubeClient
				if kcrc == nil {
					continue
				}

				err := a.createKubeClientPermissions(template.Name, kcrc, cluster, addon)
				if err != nil {
					return err
				}

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				continue

			default:
				utilruntime.HandleError(fmt.Errorf("unsupported registration type %s", registration.Type))
			}

		}

		return nil
	}
}

func (a *CRDTemplateAgentAddon) createKubeClientPermissions(
	templateName string,
	kcrc *addonapiv1alpha1.KubeClientRegistrationConfig,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) error {

	for _, pc := range kcrc.HubPermissions {
		switch pc.Type {
		case addonapiv1alpha1.HubPermissionsBindingCurrentCluster:
			if pc.CurrentCluster == nil {
				return fmt.Errorf("current cluster is required when the HubPermission type is CurrentCluster")
			}

			a.logger.V(5).Info("Set hub permission for addon",
				"addonNamespace", addon.Namespace,
				"addonName", addon.Name,
				"UID", addon.UID,
				"APIVersion", addon.APIVersion,
				"Kind", addon.Kind)

			owner := metav1.OwnerReference{
				// TODO: use apiVersion and kind in addon object, but now they could be empty at some unknown reason
				APIVersion: "addon.open-cluster-management.io/v1alpha1",
				Kind:       "ManagedClusterAddOn",
				Name:       addon.Name,
				UID:        addon.UID,
			}

			roleRef := rbacv1.RoleRef{
				Kind:     "ClusterRole",
				APIGroup: rbacv1.GroupName,
				Name:     pc.CurrentCluster.ClusterRoleName,
			}
			err := a.createPermissionBinding(templateName,
				cluster.Name, addon.Name, cluster.Name, roleRef, &owner)
			if err != nil {
				return err
			}
		case addonapiv1alpha1.HubPermissionsBindingSingleNamespace:
			if pc.SingleNamespace == nil {
				return fmt.Errorf("single namespace is required when the HubPermission type is SingleNamespace")
			}

			// set owner reference nil since the rolebinding has different namespace with the ManagedClusterAddon
			// TODO: cleanup the rolebinding when the addon is deleted
			err := a.createPermissionBinding(templateName,
				cluster.Name, addon.Name, pc.SingleNamespace.Namespace, pc.SingleNamespace.RoleRef, nil)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *CRDTemplateAgentAddon) createPermissionBinding(templateName, clusterName, addonName, namespace string,
	roleRef rbacv1.RoleRef, owner *metav1.OwnerReference) error {
	// TODO: confirm the group
	groups := agent.DefaultGroups(clusterName, addonName)
	subject := []rbacv1.Subject{}
	for _, group := range groups {
		subject = append(subject, rbacv1.Subject{
			Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: group,
		})
	}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("open-cluster-management:%s:%s:agent",
				addonName, strings.ToLower(roleRef.Kind)),
			Namespace: namespace,
			Labels: map[string]string{
				addonapiv1alpha1.AddonLabelKey: addonName,
				AddonTemplateLabelKey:          "",
			},
		},
		RoleRef:  roleRef,
		Subjects: subject,
	}
	if owner != nil {
		binding.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	_, err := a.rolebindingLister.RoleBindings(namespace).Get(binding.Name)
	switch {
	case err == nil:
		// TODO: update the rolebinding if it is not the same
		a.logger.Info("Rolebinding already exists", "rolebindingName", binding.Name)
		return nil
	case apierrors.IsNotFound(err):
		_, createErr := a.hubKubeClient.RbacV1().RoleBindings(namespace).Create(
			context.TODO(), binding, metav1.CreateOptions{})
		if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return createErr
		}
	case err != nil:
		return err
	}

	return nil
}
