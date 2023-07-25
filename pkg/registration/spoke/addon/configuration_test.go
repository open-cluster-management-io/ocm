package addon

import (
	"testing"

	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

const (
	addOnName = "addon1"
)

func TestGetRegistrationConfigs(t *testing.T) {
	addOnNamespace := "ns1"

	cases := []struct {
		name    string
		addon   *addonv1alpha1.ManagedClusterAddOn
		configs []registrationConfig
	}{
		{
			name: "no registration",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: addOnNamespace,
				},
			},
		},
		{
			name: "with default signer",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: addOnNamespace,
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonv1alpha1.RegistrationConfig{
						{
							SignerName: certificates.KubeAPIServerClientSignerName,
						},
					},
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName, "", nil, false),
			},
		},
		{
			name: "namespace in status",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonv1alpha1.RegistrationConfig{
						{
							SignerName: certificates.KubeAPIServerClientSignerName,
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName, "", nil, false),
			},
		},
		{
			name: "with default signer hosted mode",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
					Annotations: map[string]string{
						hostingClusterNameAnnotation: "test",
					},
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: addOnNamespace,
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonv1alpha1.RegistrationConfig{
						{
							SignerName: certificates.KubeAPIServerClientSignerName,
						},
					},
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName, "", nil, true),
			},
		},
		{
			name: "with customized signer",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: addOnNamespace,
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Registrations: []addonv1alpha1.RegistrationConfig{
						{
							SignerName: "mysigner",
						},
					},
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, "mysigner", "", nil, false),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			configs, err := getRegistrationConfigs(c.addon)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(configs) != len(c.configs) {
				t.Errorf("expected %d configs, but got %d", len(c.configs), len(configs))
			}

			for _, config := range c.configs {
				if _, ok := configs[config.hash]; !ok {
					t.Errorf("unexpected registrationConfig: %v", config)
				}
			}
		})
	}
}

func newRegistrationConfig(addOnName, addOnNamespace, signerName, commonName string, organization []string,
	addOnAgentRunningOutsideManagedCluster bool) registrationConfig {
	registration := addonv1alpha1.RegistrationConfig{
		SignerName: signerName,
		Subject: addonv1alpha1.Subject{
			User:   commonName,
			Groups: organization,
		},
	}
	config := registrationConfig{
		addOnName: addOnName,
		addonInstallOption: addonInstallOption{
			InstallationNamespace:             addOnNamespace,
			AgentRunningOutsideManagedCluster: addOnAgentRunningOutsideManagedCluster,
		},

		registration: registration,
	}

	hash, _ := getConfigHash(registration, config.addonInstallOption)
	config.hash = hash

	return config
}
