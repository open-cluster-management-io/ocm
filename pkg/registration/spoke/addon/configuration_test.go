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
						addonv1alpha1.HostingClusterNameAnnotationKey: "test",
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
			installOption := addonInstallOption{
				AgentRunningOutsideManagedCluster: isAddonRunningOutsideManagedCluster(c.addon),
				InstallationNamespace:             getAddOnInstallationNamespace(c.addon),
			}
			configs, err := getRegistrationConfigs(c.addon.Name, installOption, c.addon.Status.Registrations, c.addon.Status.KubeClientDriver)
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

	hash, _ := getConfigHash(registration, config.addonInstallOption, "")
	config.hash = hash

	return config
}

// TestConfigHash_StatusFieldsExcluded verifies that driver is always excluded from hash,
// and subject field is conditionally excluded based on driver type
func TestConfigHash_StatusFieldsExcluded(t *testing.T) {
	installOption := addonInstallOption{
		InstallationNamespace:             "test-ns",
		AgentRunningOutsideManagedCluster: false,
	}

	cases := []struct {
		name        string
		config1     addonv1alpha1.RegistrationConfig
		driver1     string
		config2     addonv1alpha1.RegistrationConfig
		driver2     string
		expectEqual bool
	}{
		{
			name: "driver field always excluded - csr vs token with same subject",
			config1: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver1: "csr",
			config2: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver2:     "token",
			expectEqual: false, // Different because csr includes subject, token excludes it
		},
		{
			name: "token driver excludes subject - different subjects should have same hash",
			config1: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user-1",
					Groups: []string{"test-group-1"},
				},
			},
			driver1: "token",
			config2: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user-2",
					Groups: []string{"test-group-2"},
				},
			},
			driver2:     "token",
			expectEqual: true, // Same hash because subject is excluded for token driver
		},
		{
			name: "csr driver includes subject - different subjects should have different hash",
			config1: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user-1",
					Groups: []string{"test-group-1"},
				},
			},
			driver1: "csr",
			config2: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user-2",
					Groups: []string{"test-group-2"},
				},
			},
			driver2:     "csr",
			expectEqual: false, // Different hash because subject is included for csr driver
		},
		{
			name: "csr driver includes subject - same subjects should have same hash",
			config1: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver1: "csr",
			config2: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver2:     "csr",
			expectEqual: true,
		},
		{
			name: "custom signer includes subject - different subjects should have different hash",
			config1: addonv1alpha1.RegistrationConfig{
				SignerName: "custom.signer.io/custom",
				Subject: addonv1alpha1.Subject{
					User:   "test-user-1",
					Groups: []string{"test-group-1"},
				},
			},
			driver1: "",
			config2: addonv1alpha1.RegistrationConfig{
				SignerName: "custom.signer.io/custom",
				Subject: addonv1alpha1.Subject{
					User:   "test-user-2",
					Groups: []string{"test-group-2"},
				},
			},
			driver2:     "",
			expectEqual: false, // Different hash because subject is included for custom signer
		},
		{
			name: "custom signer includes subject - same subjects should have same hash",
			config1: addonv1alpha1.RegistrationConfig{
				SignerName: "custom.signer.io/custom",
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver1: "",
			config2: addonv1alpha1.RegistrationConfig{
				SignerName: "custom.signer.io/custom",
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver2:     "",
			expectEqual: true,
		},
		{
			name: "signer name changes hash",
			config1: addonv1alpha1.RegistrationConfig{
				SignerName: certificates.KubeAPIServerClientSignerName,
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver1: "csr",
			config2: addonv1alpha1.RegistrationConfig{
				SignerName: "custom.signer.io/custom",
				Subject: addonv1alpha1.Subject{
					User:   "test-user",
					Groups: []string{"test-group"},
				},
			},
			driver2:     "",
			expectEqual: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hash1, err := getConfigHash(c.config1, installOption, c.driver1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			hash2, err := getConfigHash(c.config2, installOption, c.driver2)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if c.expectEqual {
				if hash1 != hash2 {
					t.Errorf("expected hashes to be equal, got:\nhash1=%s\nhash2=%s", hash1, hash2)
				}
			} else {
				if hash1 == hash2 {
					t.Errorf("expected hashes to differ, got same hash: %s", hash1)
				}
			}
		})
	}
}
