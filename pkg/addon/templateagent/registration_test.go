package templateagent

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2/ktesting"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestTemplateCSRConfigurationsFunc(t *testing.T) {
	cases := []struct {
		name            string
		cluster         *clusterv1.ManagedCluster
		addon           *addonapiv1alpha1.ManagedClusterAddOn
		template        *addonapiv1alpha1.AddOnTemplate
		expectedConfigs []addonapiv1alpha1.RegistrationConfig
		expectedErr     string
	}{
		{
			name:            "empty",
			cluster:         NewFakeManagedCluster("cluster1"),
			addon:           NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "", ""),
			template:        NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{}),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{},
			expectedErr:     "CSRConfigurations failed to get addon template for addon cluster1/addon1, template is nil",
		},
		{
			name:    "kubeclient",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
									RoleRef: rbacv1.RoleRef{
										APIGroup: rbacv1.GroupName,
										Kind:     "ClusterRole",
										Name:     "test",
									},
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificates.KubeAPIServerClientSignerName,
					Subject: addonapiv1alpha1.Subject{
						User: "system:open-cluster-management:cluster:cluster1:addon:addon1:agent:addon1-agent",

						Groups: []string{
							"system:open-cluster-management:cluster:cluster1:addon:addon1",
							"system:open-cluster-management:addon:addon1",
							"system:authenticated",
						},
						OrganizationUnits: []string{},
					},
				},
			},
		},
		{
			name:    "customsigner",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Name: "name1",
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "s1",
					Subject: addonapiv1alpha1.Subject{
						User: "u1",
						Groups: []string{
							"g1",
							"g2",
						},
						OrganizationUnits: []string{},
					},
				},
			},
		},
	}
	for _, c := range cases {
		_, ctx := ktesting.NewTestContext(t)
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}

		agent := NewCRDTemplateAgentAddon(ctx, c.addon.Name, nil, addonClient, addonInformerFactory, nil, nil)
		f := agent.TemplateCSRConfigurationsFunc()
		registrationConfigs, err := f(c.cluster, c.addon)
		if c.expectedErr == "" {
			if err != nil {
				t.Fatalf("case: %s, expected no error but got: %v", c.name, err)
			}
		} else {
			if err == nil {
				t.Fatalf("case: %s, expected error but got none", c.name)
			}
			if !strings.Contains(err.Error(), c.expectedErr) {
				t.Fatalf("case: %s, expected error containing %q but got: %v", c.name, c.expectedErr, err)
			}
		}
		if !equality.Semantic.DeepEqual(registrationConfigs, c.expectedConfigs) {
			t.Errorf("expected registrationConfigs %v, but got %v", c.expectedConfigs, registrationConfigs)
		}
	}
}

func TestTemplateCSRApproveCheckFunc(t *testing.T) {
	cases := []struct {
		name            string
		cluster         *clusterv1.ManagedCluster
		addon           *addonapiv1alpha1.ManagedClusterAddOn
		template        *addonapiv1alpha1.AddOnTemplate
		csr             *certificatesv1.CertificateSigningRequest
		expectedApprove bool
	}{
		{
			name:            "empty",
			cluster:         NewFakeManagedCluster("cluster1"),
			addon:           NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "", ""),
			template:        NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{}),
			expectedApprove: false,
		},
		{
			name:    "kubeclient",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
									RoleRef: rbacv1.RoleRef{
										APIGroup: rbacv1.GroupName,
										Kind:     "ClusterRole",
										Name:     "test",
									},
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: certificates.KubeAPIServerClientSignerName,
				},
			},
			expectedApprove: false, // fake csr data
		},
		{
			name:    "customsigner",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Name: "name1",
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "s1",
				},
			},
			expectedApprove: true,
		},
	}
	for _, c := range cases {
		_, ctx := ktesting.NewTestContext(t)
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}
		agent := NewCRDTemplateAgentAddon(ctx, c.addon.Name, nil, addonClient, addonInformerFactory, nil, nil)
		f := agent.TemplateCSRApproveCheckFunc()
		approve := f(c.cluster, c.addon, c.csr)
		if approve != c.expectedApprove {
			t.Errorf("expected approve result %v, but got %v", c.expectedApprove, approve)
		}
	}
}

func TestTemplateCSRSignFunc(t *testing.T) {
	ca, key, err := certutil.GenerateSelfSignedCertKey("test", []net.IP{}, []string{})
	if err != nil {
		t.Errorf("Failed to generate self signed CA config: %v", err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       ca,
			corev1.TLSPrivateKeyKey: key,
		},
		Type: corev1.SecretTypeTLS,
	}

	cases := []struct {
		name         string
		cluster      *clusterv1.ManagedCluster
		addon        *addonapiv1alpha1.ManagedClusterAddOn
		template     *addonapiv1alpha1.AddOnTemplate
		casecret     *corev1.Secret
		csr          *certificatesv1.CertificateSigningRequest
		expectedCert []byte
		expectedErr  string
	}{
		{
			name:    "kubeclient",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
									RoleRef: rbacv1.RoleRef{
										APIGroup: rbacv1.GroupName,
										Kind:     "ClusterRole",
										Name:     "test",
									},
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: certificates.KubeAPIServerClientSignerName,
					Username:   "system:open-cluster-management:cluster1:adcde",
				},
			},
			expectedCert: nil,
		},
		{
			name:    "customsigner no ca secret",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Name: "name1",
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "s1",
					Username:   "system:open-cluster-management:cluster1:adcde",
				},
			},
			expectedCert: nil,
			expectedErr:  `get custom signer ca open-cluster-management-hub/name1 failed: secrets "name1" not found`,
		},
		{
			name:     "customsigner with ca secret",
			casecret: secret,
			cluster:  NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Name:      secret.Name,
							Namespace: secret.Namespace,
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
					},
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "s1",
					Username:   "system:open-cluster-management:cluster1:adcde",
				},
			},
			expectedCert: nil,
			expectedErr:  "failed to sign csr: PEM block type must be CERTIFICATE REQUEST",
		},
	}
	for _, c := range cases {
		_, ctx := ktesting.NewTestContext(t)
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		hubKubeClient := fakekube.NewSimpleClientset()
		if c.casecret != nil {
			hubKubeClient = fakekube.NewSimpleClientset(c.casecret)
		}
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}

		agent := NewCRDTemplateAgentAddon(ctx, c.addon.Name, hubKubeClient, addonClient, addonInformerFactory, nil, nil)
		f := agent.TemplateCSRSignFunc()
		cert, err := f(c.cluster, c.addon, c.csr)
		if c.expectedErr == "" {
			if err != nil {
				t.Fatalf("case: %s, expected no error but got: %v", c.name, err)
			}
		} else {
			if err == nil {
				t.Fatalf("case: %s, expected error but got none", c.name)
			}
			if !strings.Contains(err.Error(), c.expectedErr) {
				t.Fatalf("case: %s, expected error containing %q but got: %v", c.name, c.expectedErr, err)
			}
		}
		if !bytes.Equal(cert, c.expectedCert) {
			t.Errorf("expected cert %v, but got %v", c.expectedCert, cert)
		}
	}
}

func NewFakeManagedCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1.ManagedClusterSpec{},
	}
}

func NewFakeTemplateManagedClusterAddon(name, clusterName, addonTemplateName, addonTemplateSpecHash string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := &addonapiv1alpha1.ManagedClusterAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: clusterName,
			UID:       "fakeuid",
		},
		Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{},
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			// Add default registration status with subjects for dynamic RBAC binding
			Registrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: certificatesv1.KubeAPIServerClientSignerName,
					Subject: addonapiv1alpha1.Subject{
						User: fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s:agent:%s-agent",
							clusterName, name, name),
						Groups: []string{
							fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s", clusterName, name),
							fmt.Sprintf("system:open-cluster-management:addon:%s", name),
						},
					},
				},
			},
		},
	}

	if addonTemplateName != "" {
		addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
			{
				ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
					Group:    "addon.open-cluster-management.io",
					Resource: "addontemplates",
				},
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Name: addonTemplateName,
				},
				DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Name: addonTemplateName,
					},
					SpecHash: addonTemplateSpecHash,
				},
			},
		}
	}
	return addon
}

func NewFakeAddonTemplate(name string,
	registrationSpec []addonapiv1alpha1.RegistrationSpec) *addonapiv1alpha1.AddOnTemplate {
	return &addonapiv1alpha1.AddOnTemplate{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: addonapiv1alpha1.AddOnTemplateSpec{
			Registration: registrationSpec,
		},
	}
}

func NewFakeRoleBinding(addonName, namespace string, subject []rbacv1.Subject, roleRef rbacv1.RoleRef,
	owner metav1.OwnerReference) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("open-cluster-management:%s:%s:agent",
				addonName, strings.ToLower(roleRef.Kind)),
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{owner},
			Labels: map[string]string{
				addonapiv1alpha1.AddonLabelKey: addonName,
			},
		},
		RoleRef:  roleRef,
		Subjects: subject,
	}
}

func TestTemplatePermissionConfigFunc(t *testing.T) {
	cases := []struct {
		name                   string
		cluster                *clusterv1.ManagedCluster
		addon                  *addonapiv1alpha1.ManagedClusterAddOn
		template               *addonapiv1alpha1.AddOnTemplate
		rolebinding            *rbacv1.RoleBinding
		expectedErr            error
		validatePermissionFunc func(*testing.T, kubernetes.Interface)
	}{
		{
			name:    "kubeclient current cluster binding, rolebinding not exist",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingCurrentCluster,
								CurrentCluster: &addonapiv1alpha1.CurrentClusterBindingConfig{
									ClusterRoleName: "test",
								},
							},
						},
					},
				},
			}),
			addon:       NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedErr: nil,
			validatePermissionFunc: func(t *testing.T, kubeClient kubernetes.Interface) {
				rb, err := kubeClient.RbacV1().RoleBindings("cluster1").Get(context.TODO(),
					fmt.Sprintf("open-cluster-management:%s:%s:agent", "addon1", strings.ToLower("ClusterRole")),
					metav1.GetOptions{},
				)
				if err != nil {
					t.Errorf("failed to get rolebinding: %v", err)
				}

				if rb.RoleRef.Name != "test" {
					t.Errorf("expected rolebinding %s, got %s", "test", rb.RoleRef.Name)
				}
				if rb.RoleRef.Kind != "ClusterRole" {
					t.Errorf("expected rolebinding kind %s, got %s", "ClusterRole", rb.RoleRef.Kind)
				}
				if len(rb.OwnerReferences) != 1 {
					t.Errorf("expected rolebinding to have 1 owner reference, got %d", len(rb.OwnerReferences))
				}
				if rb.OwnerReferences[0].Kind != "ManagedClusterAddOn" {
					t.Errorf("expected rolebinding owner reference kind to be ManagedClusterAddOn, got %s",
						rb.OwnerReferences[0].Kind)
				}
				if rb.OwnerReferences[0].Name != "addon1" {
					t.Errorf("expected rolebinding owner reference name to be addon1, got %s",
						rb.OwnerReferences[0].Name)
				}
			},
		},
		{
			name:    "kubeclient current cluster binding, rolebinding exists",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingCurrentCluster,
								CurrentCluster: &addonapiv1alpha1.CurrentClusterBindingConfig{
									ClusterRoleName: "test",
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			rolebinding: NewFakeRoleBinding("addon1", "cluster1",
				[]rbacv1.Subject{{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:authenticated"},
				}, rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "test",
				},
				metav1.OwnerReference{
					APIVersion: "addon.open-cluster-management.io/v1alpha1",
					Kind:       "ManagedClusterAddOn",
					Name:       "addon1",
					UID:        "fakeuid",
				}),
			expectedErr: nil,
			validatePermissionFunc: func(t *testing.T, kubeClient kubernetes.Interface) {
				rb, err := kubeClient.RbacV1().RoleBindings("cluster1").Get(context.TODO(),
					fmt.Sprintf("open-cluster-management:%s:%s:agent", "addon1", strings.ToLower("ClusterRole")),
					metav1.GetOptions{},
				)
				if err != nil {
					t.Errorf("failed to get rolebinding: %v", err)
				}

				if rb.RoleRef.Name != "test" {
					t.Errorf("expected rolebinding %s, got %s", "test", rb.RoleRef.Name)
				}
				if rb.RoleRef.Kind != "ClusterRole" {
					t.Errorf("expected rolebinding kind %s, got %s", "ClusterRole", rb.RoleRef.Kind)
				}
				if len(rb.OwnerReferences) != 1 {
					t.Errorf("expected rolebinding to have 1 owner reference, got %d", len(rb.OwnerReferences))
				}
				if rb.OwnerReferences[0].Kind != "ManagedClusterAddOn" {
					t.Errorf("expected rolebinding owner reference kind to be ManagedClusterAddOn, got %s",
						rb.OwnerReferences[0].Kind)
				}
				if rb.OwnerReferences[0].Name != "addon1" {
					t.Errorf("expected rolebinding owner reference name to be addon1, got %s",
						rb.OwnerReferences[0].Name)
				}
			},
		},
		{
			name:    "kubeclient single namespace binding",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
									RoleRef: rbacv1.RoleRef{
										APIGroup: rbacv1.GroupName,
										Kind:     "ClusterRole",
										Name:     "test",
									},
								},
							},
						},
					},
				},
			}),
			addon:       NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedErr: nil,
			validatePermissionFunc: func(t *testing.T, kubeClient kubernetes.Interface) {
				rb, err := kubeClient.RbacV1().RoleBindings("test").Get(context.TODO(),
					fmt.Sprintf("open-cluster-management:%s:%s:agent", "addon1", strings.ToLower("ClusterRole")),
					metav1.GetOptions{},
				)
				if err != nil {
					t.Errorf("failed to get rolebinding: %v", err)
				}

				if rb.RoleRef.Name != "test" {
					t.Errorf("expected rolebinding %s, got %s", "test", rb.RoleRef.Name)
				}
				if len(rb.OwnerReferences) != 0 {
					t.Errorf("expected rolebinding to have 0 owner reference, got %d", len(rb.OwnerReferences))
				}
				// Dynamic subjects: 1 user + 2 groups
				if len(rb.Subjects) != 3 {
					t.Errorf("expected rolebinding to have 3 subjects, got %d", len(rb.Subjects))
				}
				// Verify user subject
				if rb.Subjects[0].Kind != rbacv1.UserKind {
					t.Errorf("expected first subject to be User, got %s", rb.Subjects[0].Kind)
				}
				if rb.Subjects[0].Name != "system:open-cluster-management:cluster:cluster1:addon:addon1:agent:addon1-agent" {
					t.Errorf("expected user subject name to be system:open-cluster-management:cluster:cluster1:addon:addon1:agent:addon1-agent, got %s",
						rb.Subjects[0].Name)
				}
				// Verify group subjects
				if rb.Subjects[1].Kind != rbacv1.GroupKind {
					t.Errorf("expected second subject to be Group, got %s", rb.Subjects[1].Kind)
				}
				if rb.Subjects[2].Kind != rbacv1.GroupKind {
					t.Errorf("expected third subject to be Group, got %s", rb.Subjects[2].Kind)
				}
			},
		},
		{
			name:    "customsigner",
			cluster: NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Name: "name1",
						},
					},
				},
			}),
			addon:       NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedErr: nil,
		},
	}
	for _, c := range cases {
		_, ctx := ktesting.NewTestContext(t)
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		hubKubeClient := fakekube.NewSimpleClientset()
		if c.rolebinding != nil {
			hubKubeClient = fakekube.NewSimpleClientset(c.rolebinding)
		}
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}
		kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute)
		if c.rolebinding != nil {

			rbStore := kubeInformers.Rbac().V1().RoleBindings().Informer().GetStore()
			if err := rbStore.Add(c.rolebinding); err != nil {
				t.Fatal(err)
			}
		}

		agent := NewCRDTemplateAgentAddon(ctx, c.addon.Name, hubKubeClient, addonClient, addonInformerFactory,
			kubeInformers.Rbac().V1().RoleBindings().Lister())
		f := agent.TemplatePermissionConfigFunc()
		err := f(c.cluster, c.addon)
		if err != c.expectedErr {
			t.Errorf("expected registrationConfigs %v, but got %v", c.expectedErr, err)
		}
		if c.validatePermissionFunc != nil {
			c.validatePermissionFunc(t, hubKubeClient)
		}
	}
}

func TestAddonManagerNamespace(t *testing.T) {
	cases := []struct {
		name         string
		podNamespace string
		envNs        string
		expected     string
	}{
		{
			name:         "pod namespace is not empty",
			podNamespace: "test",
			envNs:        "",
			expected:     "test",
		},
		{
			name:         "pod namespace is empty, env is not empty",
			podNamespace: "",
			envNs:        "test-env",
			expected:     "test-env",
		},
		{
			name:         "default namespace",
			podNamespace: "",
			envNs:        "",
			expected:     "open-cluster-management-hub",
		},
	}

	for _, c := range cases {
		if c.podNamespace != "" {
			podNamespace = c.podNamespace
		}
		if c.envNs != "" {
			os.Setenv("POD_NAMESPACE", c.envNs)
		}
		ns := AddonManagerNamespace()
		assert.Equal(t, c.expected, ns)
		// reset podNamespace and env
		podNamespace = ""
		os.Setenv("POD_NAMESPACE", "")
	}
}

func TestBuildSubjectsFromRegistration(t *testing.T) {
	cases := []struct {
		name             string
		addon            *addonapiv1alpha1.ManagedClusterAddOn
		signerName       string
		expectedSubjects []rbacv1.Subject
	}{
		{
			name: "no registrations",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{},
			},
			signerName:       certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: nil,
		},
		{
			name: "registration with user and groups",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject: addonapiv1alpha1.Subject{
								User: "system:open-cluster-management:cluster:cluster1:addon:test-addon:agent:test-addon-agent",
								Groups: []string{
									"system:open-cluster-management:cluster:cluster1:addon:test-addon",
									"system:open-cluster-management:addon:test-addon",
								},
							},
						},
					},
				},
			},
			signerName: certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: []rbacv1.Subject{
				{
					Kind:     rbacv1.UserKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:open-cluster-management:cluster:cluster1:addon:test-addon:agent:test-addon-agent",
				},
				{
					Kind:     rbacv1.GroupKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:open-cluster-management:cluster:cluster1:addon:test-addon",
				},
				{
					Kind:     rbacv1.GroupKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:open-cluster-management:addon:test-addon",
				},
			},
		},
		{
			name: "registration with only groups",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject: addonapiv1alpha1.Subject{
								Groups: []string{
									"system:open-cluster-management:cluster:cluster1:addon:test-addon",
								},
							},
						},
					},
				},
			},
			signerName: certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: []rbacv1.Subject{
				{
					Kind:     rbacv1.GroupKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:open-cluster-management:cluster:cluster1:addon:test-addon",
				},
			},
		},
		{
			name: "registration with only user",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject: addonapiv1alpha1.Subject{
								User: "system:serviceaccount:cluster1:test-addon-sa",
							},
						},
					},
				},
			},
			signerName: certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: []rbacv1.Subject{
				{
					Kind:     rbacv1.UserKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:serviceaccount:cluster1:test-addon-sa",
				},
			},
		},
		{
			name: "empty subject",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject:    addonapiv1alpha1.Subject{},
						},
					},
				},
			},
			signerName:       certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: nil,
		},
		{
			name: "signer name not found",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: "custom.signer/name",
							Subject: addonapiv1alpha1.Subject{
								User: "test-user",
							},
						},
					},
				},
			},
			signerName:       certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: nil,
		},
		{
			name: "multiple registrations - finds correct one",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: "custom.signer/name",
							Subject: addonapiv1alpha1.Subject{
								User: "custom-user",
							},
						},
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject: addonapiv1alpha1.Subject{
								User: "kube-user",
								Groups: []string{
									"kube-group",
								},
							},
						},
					},
				},
			},
			signerName: certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: []rbacv1.Subject{
				{
					Kind:     rbacv1.UserKind,
					APIGroup: rbacv1.GroupName,
					Name:     "kube-user",
				},
				{
					Kind:     rbacv1.GroupKind,
					APIGroup: rbacv1.GroupName,
					Name:     "kube-group",
				},
			},
		},
		{
			name: "groups with system:authenticated should be filtered out",
			addon: &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-addon",
					Namespace: "cluster1",
				},
				Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonapiv1alpha1.RegistrationConfig{
						{
							SignerName: certificatesv1.KubeAPIServerClientSignerName,
							Subject: addonapiv1alpha1.Subject{
								User: "system:serviceaccount:cluster1:test-addon-sa",
								Groups: []string{
									"system:authenticated",
									"system:open-cluster-management:cluster:cluster1:addon:test-addon",
									"system:serviceaccounts",
								},
							},
						},
					},
				},
			},
			signerName: certificatesv1.KubeAPIServerClientSignerName,
			expectedSubjects: []rbacv1.Subject{
				{
					Kind:     rbacv1.UserKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:serviceaccount:cluster1:test-addon-sa",
				},
				{
					Kind:     rbacv1.GroupKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:open-cluster-management:cluster:cluster1:addon:test-addon",
				},
				{
					Kind:     rbacv1.GroupKind,
					APIGroup: rbacv1.GroupName,
					Name:     "system:serviceaccounts",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			subjects := buildSubjectsFromRegistration(c.addon, c.signerName)
			if !equality.Semantic.DeepEqual(subjects, c.expectedSubjects) {
				t.Errorf("expected subjects %v, got %v", c.expectedSubjects, subjects)
			}
		})
	}
}
