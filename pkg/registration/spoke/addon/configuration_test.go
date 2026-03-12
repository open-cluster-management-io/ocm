package addon

import (
	"testing"

	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

const (
	addOnName = "addon1"
)

func TestGetRegistrationConfigs(t *testing.T) {
	addOnNamespace := "ns1"

	cases := []struct {
		name    string
		addon   *addonv1beta1.ManagedClusterAddOn
		configs []registrationConfig
	}{
		{
			name: "no registration",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
			},
		},
		{
			name: "with default signer",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.KubeClient,
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(
					addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName, "",
					addonv1beta1.KubeClient, nil, false),
			},
		},
		{
			name: "namespace in status",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.KubeClient,
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName, "",
					addonv1beta1.KubeClient, nil, false),
			},
		},
		{
			name: "with default signer hosted mode",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
					Annotations: map[string]string{
						addonv1beta1.HostingClusterNameAnnotationKey: "test",
					},
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.KubeClient,
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName, "",
					addonv1beta1.KubeClient, nil, true),
			},
		},
		{
			name: "with customized signer",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.CustomSigner,
							CustomSigner: &addonv1beta1.CustomSignerConfig{
								SignerName: "mysigner",
							},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, "mysigner", "",
					addonv1beta1.CustomSigner, nil, false),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			installOption := addonInstallOption{
				AgentRunningOutsideManagedCluster: isAddonRunningOutsideManagedCluster(c.addon),
				InstallationNamespace:             getAddOnInstallationNamespace(c.addon),
			}
			configs, err := getRegistrationConfigs(c.addon.Name, installOption, c.addon.Status.Registrations)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(configs) != len(c.configs) {
				t.Errorf("expected %d configs, but got %d", len(c.configs), len(configs))
			}

			for _, config := range c.configs {
				if _, ok := configs[config.hash]; !ok {
					t.Errorf("unexpected registrationConfig: %v, got %v", configs, c.configs)
				}
			}
		})
	}
}

func newRegistrationConfig(
	addOnName, addOnNamespace, signerName, commonName string,
	registrationType addonv1beta1.RegistrationType,
	organization []string,
	addOnAgentRunningOutsideManagedCluster bool) registrationConfig {

	registration := addonv1beta1.RegistrationConfig{
		Type: registrationType,
	}
	switch registrationType {
	case addonv1beta1.KubeClient:
		if commonName != "" {
			registration.KubeClient = &addonv1beta1.KubeClientConfig{
				Subject: addonv1beta1.KubeClientSubject{
					BaseSubject: addonv1beta1.BaseSubject{
						User: commonName,
					},
				},
			}
		}
	case addonv1beta1.CustomSigner:
		registration.CustomSigner = &addonv1beta1.CustomSignerConfig{
			SignerName: signerName,
			Subject: addonv1beta1.Subject{
				BaseSubject: addonv1beta1.BaseSubject{
					User: commonName,
				},
				OrganizationUnits: organization,
			},
		}
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

// TestConfigHash_StatusFieldsExcluded verifies that driver is always excluded from hash,
// and subject field is conditionally excluded based on driver type
func TestConfigHash_StatusFieldsExcluded(t *testing.T) {
	installOption := addonInstallOption{
		InstallationNamespace:             "test-ns",
		AgentRunningOutsideManagedCluster: false,
	}

	cases := []struct {
		name        string
		config1     addonv1beta1.RegistrationConfig
		config2     addonv1beta1.RegistrationConfig
		expectEqual bool
	}{
		{
			name: "driver field always excluded - csr vs token with same subject",
			config1: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "token",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			config2: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			expectEqual: false, // Different because csr includes subject, token excludes it
		},
		{
			name: "csr driver includes subject - different subjects should have different hash",
			config1: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user-1",
							Groups: []string{"test-group-1"},
						},
					},
				},
			},
			config2: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user-2",
							Groups: []string{"test-group-2"},
						},
					},
				},
			},
			expectEqual: false, // Different hash because subject is included for csr driver
		},
		{
			name: "csr driver includes subject - same subjects should have same hash",
			config1: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			config2: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			expectEqual: true,
		},
		{
			name: "custom signer includes subject - different subjects should have different hash",
			config1: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/custom",
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user-1",
							Groups: []string{"test-group-1"},
						},
					},
				},
			},
			config2: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/custom",
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user-2",
							Groups: []string{"test-group-2"},
						},
					},
				},
			},
			expectEqual: false, // Different hash because subject is included for custom signer
		},
		{
			name: "custom signer includes subject - same subjects should have same hash",
			config1: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/custom",
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			config2: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/custom",
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			expectEqual: true,
		},
		{
			name: "signer name changes hash",
			config1: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/custom-1",
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			config2: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/custom-2",
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "test-user",
							Groups: []string{"test-group"},
						},
					},
				},
			},
			expectEqual: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hash1, err := getConfigHash(c.config1, installOption)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			hash2, err := getConfigHash(c.config2, installOption)
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
