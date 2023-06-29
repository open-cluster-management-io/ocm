package templateagent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestTemplateCSRConfigurationsFunc(t *testing.T) {
	cases := []struct {
		name            string
		agentName       string
		cluster         *clusterv1.ManagedCluster
		addon           *addonapiv1alpha1.ManagedClusterAddOn
		template        *addonapiv1alpha1.AddOnTemplate
		expectedConfigs []addonapiv1alpha1.RegistrationConfig
	}{
		{
			name:            "empty",
			agentName:       "agent1",
			cluster:         NewFakeManagedCluster("cluster1"),
			addon:           NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "", ""),
			template:        NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{}),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{},
		},
		{
			name:      "kubeclient",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "kubernetes.io/kube-apiserver-client",
					Subject: addonapiv1alpha1.Subject{
						User: "system:open-cluster-management:cluster:cluster1:addon:addon1:agent:agent1",

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
			name:      "customsigner",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
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
							Namespace: "ns1",
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

		agent := NewCRDTemplateAgentAddon(c.addon.Name, c.agentName, nil, addonClient, addonInformerFactory, nil, nil)
		f := agent.TemplateCSRConfigurationsFunc()
		registrationConfigs := f(c.cluster)
		if !equality.Semantic.DeepEqual(registrationConfigs, c.expectedConfigs) {
			t.Errorf("expected registrationConfigs %v, but got %v", c.expectedConfigs, registrationConfigs)
		}
	}
}

func TestTemplateCSRApproveCheckFunc(t *testing.T) {
	cases := []struct {
		name            string
		agentName       string
		cluster         *clusterv1.ManagedCluster
		addon           *addonapiv1alpha1.ManagedClusterAddOn
		template        *addonapiv1alpha1.AddOnTemplate
		csr             *certificatesv1.CertificateSigningRequest
		expectedApprove bool
	}{
		{
			name:            "empty",
			agentName:       "agent1",
			cluster:         NewFakeManagedCluster("cluster1"),
			addon:           NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "", ""),
			template:        NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{}),
			expectedApprove: false,
		},
		{
			name:      "kubeclient",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
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
					SignerName: "kubernetes.io/kube-apiserver-client",
				},
			},
			expectedApprove: false, // fake csr data
		},
		{
			name:      "customsigner",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
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
							Namespace: "ns1",
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
		agent := NewCRDTemplateAgentAddon(c.addon.Name, c.agentName, nil, addonClient, addonInformerFactory, nil, nil)
		f := agent.TemplateCSRApproveCheckFunc()
		approve := f(c.cluster, c.addon, c.csr)
		if approve != c.expectedApprove {
			t.Errorf("expected approve result %v, but got %v", c.expectedApprove, approve)
		}
	}
}

func TestTemplateCSRSignFunc(t *testing.T) {
	cases := []struct {
		name         string
		agentName    string
		cluster      *clusterv1.ManagedCluster
		addon        *addonapiv1alpha1.ManagedClusterAddOn
		template     *addonapiv1alpha1.AddOnTemplate
		csr          *certificatesv1.CertificateSigningRequest
		expectedCert []byte
	}{
		{
			name:      "kubeclient",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
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
					SignerName: "kubernetes.io/kube-apiserver-client",
					Username:   "system:open-cluster-management:cluster1:adcde",
				},
			},
			expectedCert: nil,
		},
		{
			name:      "customsigner no ca secret",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
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
							Namespace: "ns1",
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
		},
	}
	for _, c := range cases {
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		hubKubeClient := fakekube.NewSimpleClientset()
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}

		agent := NewCRDTemplateAgentAddon(c.addon.Name, c.agentName, hubKubeClient, addonClient, addonInformerFactory, nil, nil)
		f := agent.TemplateCSRSignFunc()
		cert := f(c.csr)
		if !bytes.Equal(cert, c.expectedCert) {
			t.Errorf("expected cert %v, but got %v", c.expectedCert, cert)
		}
	}
}

func NewFakeManagedCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: clusterv1.SchemeGroupVersion.String(),
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
		},
		Spec:   addonapiv1alpha1.ManagedClusterAddOnSpec{},
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{},
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
		agentName              string
		cluster                *clusterv1.ManagedCluster
		addon                  *addonapiv1alpha1.ManagedClusterAddOn
		template               *addonapiv1alpha1.AddOnTemplate
		rolebinding            *rbacv1.RoleBinding
		expectedErr            error
		validatePermissionFunc func(*testing.T, kubernetes.Interface)
	}{
		{
			name:      "kubeclient current cluster binding",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingCurrentCluster,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "Role",
									Name:     "test",
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
					Kind:     "Role",
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
					fmt.Sprintf("open-cluster-management:%s:%s:agent", "addon1", strings.ToLower("Role")),
					metav1.GetOptions{},
				)
				if err != nil {
					t.Errorf("failed to get rolebinding: %v", err)
				}

				if rb.RoleRef.Name != "test" {
					t.Errorf("expected rolebinding %s, got %s", "test", rb.RoleRef.Name)
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
			name:      "kubeclient single namespace binding",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
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
			},
		},
		{
			name:      "customsigner",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
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
							Namespace: "ns1",
						},
					},
				},
			}),
			addon:       NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedErr: nil,
		},
	}
	for _, c := range cases {
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

		agent := NewCRDTemplateAgentAddon(c.addon.Name, c.agentName, hubKubeClient, addonClient, addonInformerFactory,
			kubeInformers.Rbac().V1().RoleBindings().Lister(), nil)
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
