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
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/pkg/errors"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	commonrecorder "open-cluster-management.io/ocm/pkg/common/recorder"
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

// GetDesiredAddOnTemplate returns the desired AddOnTemplate for the given ManagedClusterAddOn.
// If the desired AddOnTemplate is not found in the ManagedClusterAddOn Status ConfigReferences,
// it will return a nil AddOnTemplate with no error. the caller should handle the nil
// AddOnTemplate case.
func (a *CRDTemplateAgentAddon) GetDesiredAddOnTemplate(addon *addonapiv1beta1.ManagedClusterAddOn,
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
	var configReferences []addonapiv1beta1.ConfigReference
	for _, configReference := range cma.Status.DefaultConfigReferences {
		configReferences = append(configReferences, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: configReference.ConfigGroupResource,
			DesiredConfig:       configReference.DesiredConfig,
		})
	}
	return a.getDesiredAddOnTemplateInner(cma.Name, configReferences)
}

func (a *CRDTemplateAgentAddon) TemplateCSRConfigurationsFunc() agent.RegistrationConfigurationsFunc {

	return func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn,
	) ([]agent.RegistrationConfig, error) {
		template, err := a.GetDesiredAddOnTemplate(addon, cluster.Name, a.addonName)
		if err != nil {
			return nil, fmt.Errorf("CSRConfigurations failed to get addon template for addon %s/%s: %v",
				cluster.Name, a.addonName, err)
		}
		if template == nil {
			return nil, fmt.Errorf("CSRConfigurations failed to get addon template for addon %s/%s, template is nil",
				cluster.Name, a.addonName)
		}

		registrationConfigs := make([]agent.RegistrationConfig, 0)
		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:
				config := &agent.KubeClientRegistration{
					User:   agent.DefaultUser(cluster.Name, a.addonName, a.agentName),
					Groups: agent.DefaultGroups(cluster.Name, a.addonName),
				}
				registrationConfigs = append(registrationConfigs, config)
			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				if registration.CustomSigner == nil {
					continue
				}
				config := &agent.CustomSignerRegistration{
					SignerName: registration.CustomSigner.SignerName,
					User:       agent.DefaultUser(cluster.Name, a.addonName, a.agentName),
					Groups:     agent.DefaultGroups(cluster.Name, a.addonName),
				}
				if registration.CustomSigner.Subject != nil {
					config.User = registration.CustomSigner.Subject.User
					config.Groups = registration.CustomSigner.Subject.Groups
					config.OrganizationUnits = registration.CustomSigner.Subject.OrganizationUnits
				}
				registrationConfigs = append(registrationConfigs, config)
			default:
				a.logger.Info("CSRConfigurations unsupported registration type",
					"clusterName", cluster.Name, "addonName", a.addonName, "type", registration.Type)
			}
		}

		return registrationConfigs, nil
	}
}

func (a *CRDTemplateAgentAddon) TemplateCSRApproveCheckFunc() agent.CSRApproveFunc {

	return func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {

		template, err := a.GetDesiredAddOnTemplate(addon, cluster.Name, a.addonName)
		if err != nil {
			a.logger.Info("CSRApproveCheck failed to get addon template",
				"clusterName", cluster.Name, "addonName", a.addonName, "error", err)
			return false
		}
		if template == nil {
			a.logger.Info("CSRApproveCheck failed to get addon template, template is nil",
				"clusterName", cluster.Name, "addonName", a.addonName)
			return false
		}

		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:

				if csr.Spec.SignerName == certificatesv1.KubeAPIServerClientSignerName {
					return KubeClientCSRApprover(a.agentName)(ctx, cluster, addon, csr)
				}

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				if registration.CustomSigner == nil {
					continue
				}
				if csr.Spec.SignerName == registration.CustomSigner.SignerName {
					return CustomerSignerCSRApprover(a.logger, a.addonName)(ctx, cluster, addon, csr)
				}

			default:
				a.logger.Info("CSRApproveCheck unsupported registration type",
					"clusterName", cluster.Name, "addonName", a.addonName, "type", registration.Type)
			}

		}

		return false
	}
}

// KubeClientCSRApprover approve the csr when addon agent uses default group, default user and
// "kubernetes.io/kube-apiserver-client" signer to sign csr.
func KubeClientCSRApprover(agentName string) agent.CSRApproveFunc {
	return func(
		ctx context.Context,
		cluster *clusterv1.ManagedCluster,
		addon *addonapiv1beta1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {
		if csr.Spec.SignerName != certificatesv1.KubeAPIServerClientSignerName {
			return false
		}
		return utils.DefaultCSRApprover(agentName)(ctx, cluster, addon, csr)
	}
}

// CustomerSignerCSRApprover approve the csr when addon agent uses custom signer to sign csr.
func CustomerSignerCSRApprover(logger klog.Logger, agentName string) agent.CSRApproveFunc {
	return func(
		ctx context.Context,
		cluster *clusterv1.ManagedCluster,
		addon *addonapiv1beta1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) bool {

		logger.Info("Customer signer CSR is approved",
			"clusterName", cluster.Name,
			"addonName", addon.Name,
			"requester", csr.Spec.Username)
		return true
	}
}

func (a *CRDTemplateAgentAddon) TemplateCSRSignFunc() agent.CSRSignerFunc {

	return func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) ([]byte, error) {
		template, err := a.GetDesiredAddOnTemplate(addon, cluster.Name, a.addonName)
		if err != nil {
			return nil, fmt.Errorf("CSRSign failed to get template for addon %s/%s: %v",
				cluster.Name, a.addonName, err)
		}
		if template == nil {
			return nil, fmt.Errorf("CSRSign failed to get addon template for addon %s/%s, template is nil",
				cluster.Name, a.addonName)
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
					return CustomSignerWithExpiry(a.hubKubeClient, registration.CustomSigner, 24*time.Hour)(ctx, cluster, addon, csr)
				}

			default:
				a.logger.Info("CSRSign unsupported registration type",
					"clusterName", cluster.Name, "addonName", a.addonName, "type", registration.Type)
			}

		}

		return nil, nil
	}
}

func CustomSignerWithExpiry(
	kubeclient kubernetes.Interface,
	customSignerConfig *addonapiv1alpha1.CustomSignerRegistrationConfig,
	duration time.Duration,
) agent.CSRSignerFunc {
	return func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn,
		csr *certificatesv1.CertificateSigningRequest) ([]byte, error) {
		if customSignerConfig == nil {
			return nil, fmt.Errorf("custom signer config is nil")
		}

		if csr.Spec.SignerName != customSignerConfig.SignerName {
			return nil, nil
		}

		secretNamespace := AddonManagerNamespace()
		if len(customSignerConfig.SigningCA.Namespace) != 0 {
			secretNamespace = customSignerConfig.SigningCA.Namespace
		}
		caSecret, err := kubeclient.CoreV1().Secrets(secretNamespace).Get(
			ctx, customSignerConfig.SigningCA.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get custom signer ca %s/%s failed: %w",
				secretNamespace, customSignerConfig.SigningCA.Name, err)
		}

		caData, caKey, err := extractCAdata(caSecret.Data[corev1.TLSCertKey], caSecret.Data[corev1.TLSPrivateKeyKey])
		if err != nil {
			return nil, fmt.Errorf("get ca %s/%s data failed: %w",
				secretNamespace, customSignerConfig.SigningCA.Name, err)
		}
		return utils.DefaultSignerWithExpiry(caKey, caData, duration)(ctx, cluster, addon, csr)
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
		if errPkcs1 != nil {
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
// clusterRole/role.
// It uses dynamic subject binding which works with both CSR-based and token-based authentication.
func (a *CRDTemplateAgentAddon) TemplatePermissionConfigFunc() agent.PermissionConfigFunc {

	return func(ctx context.Context, cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {
		template, err := a.GetDesiredAddOnTemplate(addon, cluster.Name, a.addonName)
		if err != nil {
			return fmt.Errorf("PermissionConfig failed to get addon template for addon %s/%s: %v",
				cluster.Name, a.addonName, err)
		}
		if template == nil {
			return fmt.Errorf("PermissionConfig failed to get addon template for addon %s/%s, template is nil",
				cluster.Name, a.addonName)
		}

		for _, registration := range template.Spec.Registration {
			switch registration.Type {
			case addonapiv1alpha1.RegistrationTypeKubeClient:
				kcrc := registration.KubeClient
				if kcrc == nil {
					continue
				}

				err := a.createKubeClientPermissions(kcrc, cluster, addon)
				if err != nil {
					return err
				}

			case addonapiv1alpha1.RegistrationTypeCustomSigner:
				continue

			default:
				a.logger.Info("PermissionConfig unsupported registration type",
					"clusterName", cluster.Name, "addonName", a.addonName, "type", registration.Type)
			}

		}

		return nil
	}
}

func (a *CRDTemplateAgentAddon) createKubeClientPermissions(
	kcrc *addonapiv1alpha1.KubeClientRegistrationConfig,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn,
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
				APIVersion: addonapiv1beta1.GroupVersion.String(),
				Kind:       "ManagedClusterAddOn",
				Name:       addon.Name,
				UID:        addon.UID,
			}

			roleRef := rbacv1.RoleRef{
				Kind:     "ClusterRole",
				APIGroup: rbacv1.GroupName,
				Name:     pc.CurrentCluster.ClusterRoleName,
			}
			err := a.createPermissionBinding(cluster.Name, addon.Name, cluster.Name, roleRef, &owner)
			if err != nil {
				return err
			}
		case addonapiv1alpha1.HubPermissionsBindingSingleNamespace:
			if pc.SingleNamespace == nil {
				return fmt.Errorf("single namespace is required when the HubPermission type is SingleNamespace")
			}

			// set owner reference nil since the rolebinding has different namespace with the ManagedClusterAddon
			// TODO: cleanup the rolebinding when the addon is deleted
			err := a.createPermissionBinding(cluster.Name, addon.Name,
				pc.SingleNamespace.Namespace, pc.SingleNamespace.RoleRef, nil)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *CRDTemplateAgentAddon) createPermissionBinding(clusterName, addonName, namespace string,
	roleRef rbacv1.RoleRef, owner *metav1.OwnerReference) error {

	// Get the ManagedClusterAddOn to extract dynamic subjects from Status.Registrations
	addon, err := a.addonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if err != nil {
		return fmt.Errorf("failed to get ManagedClusterAddOn %s/%s: %w", clusterName, addonName, err)
	}

	// Build subjects dynamically from addon.Status.Registrations for KubeClient signer
	// This works with both CSR-based and token-based authentication
	subjects := buildSubjectsFromRegistration(addon)

	// If no subjects found, return pending error to retry later
	// This can happen when the addon is first created and registrations are not yet populated
	if len(subjects) == 0 {
		return &agent.SubjectNotReadyError{}
	}

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("open-cluster-management:%s:%s:agent",
				addonName, strings.ToLower(roleRef.Kind)),
			Namespace: namespace,
			Labels: map[string]string{
				addonapiv1beta1.AddonLabelKey: addonName,
				AddonTemplateLabelKey:         "",
			},
		},
		RoleRef:  roleRef,
		Subjects: subjects,
	}
	if owner != nil {
		binding.OwnerReferences = []metav1.OwnerReference{*owner}
	}

	// TODO(qiujian16) this should have ctx passed to build the wrapper
	recorderWrapper := commonrecorder.NewEventsRecorderWrapper(
		context.Background(),
		events.NewContextualLoggingEventRecorder(fmt.Sprintf("addontemplate-%s-%s", clusterName, addonName)),
	)

	_, modified, err := resourceapply.ApplyRoleBinding(context.TODO(),
		a.hubKubeClient.RbacV1(), recorderWrapper, binding)
	if err == nil && modified {
		a.logger.Info("Rolebinding for addon updated", "namespace", binding.Namespace, "name", binding.Name,
			"clusterName", clusterName, "addonName", addonName, "subjects", subjects)
	}
	return err
}

// buildSubjectsFromRegistration extracts and builds RBAC subjects from addon registration status.
// Returns nil if no matching registration is found or if the subject is empty.
// This function is based on the addon-framework implementation to support dynamic subject binding
// for both CSR-based and token-based authentication.
func buildSubjectsFromRegistration(addon *addonapiv1beta1.ManagedClusterAddOn) []rbacv1.Subject {
	// Find the registration config for the specified signer
	var kubeClientConfig *addonapiv1beta1.KubeClientConfig
	for _, registration := range addon.Status.Registrations {
		if registration.Type == addonapiv1beta1.KubeClient {
			kubeClientConfig = registration.KubeClient
			break
		}
	}

	// If no registration config found or subject is empty, return nil
	if kubeClientConfig == nil || equality.Semantic.DeepEqual(kubeClientConfig.Subject, addonapiv1beta1.KubeClientSubject{}) {
		return nil
	}

	// Build subjects from the registration config subject
	subjects := []rbacv1.Subject{}

	// Include user if set
	if kubeClientConfig.Subject.User != "" {
		subjects = append(subjects, rbacv1.Subject{
			Kind:     rbacv1.UserKind,
			APIGroup: rbacv1.GroupName,
			Name:     kubeClientConfig.Subject.User,
		})
	}

	// Include groups
	for _, group := range kubeClientConfig.Subject.Groups {
		if group == "system:authenticated" {
			continue
		}
		subjects = append(subjects, rbacv1.Subject{
			Kind:     rbacv1.GroupKind,
			APIGroup: rbacv1.GroupName,
			Name:     group,
		})
	}

	return subjects
}
