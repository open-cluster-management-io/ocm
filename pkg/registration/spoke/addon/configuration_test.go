package addon

import (
	"testing"

	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/dump"
	"k8s.io/klog/v2"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
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
			name: "with kubeClient signer",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type:       addonv1beta1.KubeClient,
							KubeClient: &addonv1beta1.KubeClientConfig{Driver: "csr"},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(
					addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName,
					csr.DefaultCommonName(testinghelpers.TestManagedClusterName, addOnName), "csr",
					addonv1beta1.KubeClient,
					[]string{csr.DefaultOrganization(testinghelpers.TestManagedClusterName, addOnName)}, nil, false),
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
							Type:       addonv1beta1.KubeClient,
							KubeClient: &addonv1beta1.KubeClientConfig{Driver: "csr"},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName,
					csr.DefaultCommonName(testinghelpers.TestManagedClusterName, addOnName), "csr",
					addonv1beta1.KubeClient, []string{csr.DefaultOrganization(testinghelpers.TestManagedClusterName, addOnName)}, nil, true),
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
								Subject: addonv1beta1.Subject{
									BaseSubject: addonv1beta1.BaseSubject{
										User: "user-1",
									},
								},
							},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, "mysigner", "user-1", "",
					addonv1beta1.CustomSigner, nil, nil, false),
			},
		},
		{
			name: "KubeClient with nil config - should skip",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type:       addonv1beta1.KubeClient,
							KubeClient: nil,
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{},
		},
		{
			name: "CustomSigner with nil config - should skip",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type:         addonv1beta1.CustomSigner,
							CustomSigner: nil,
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{},
		},
		{
			name: "CustomSigner with empty SignerName - should skip",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.CustomSigner,
							CustomSigner: &addonv1beta1.CustomSignerConfig{
								SignerName: "",
							},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{},
		},
		{
			name: "Unsupported registration type - should skip",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: "UnsupportedType",
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{},
		},
		{
			name: "Token driver config",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.KubeClient,
							KubeClient: &addonv1beta1.KubeClientConfig{
								Driver: "token",
							},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(
					addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName,
					token.TokenSubject(testinghelpers.TestManagedClusterName, addOnName).User, "token",
					addonv1beta1.KubeClient, nil, nil, false),
			},
		},
		{
			name: "Mixed valid and invalid configs",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type:       addonv1beta1.KubeClient,
							KubeClient: nil, // invalid
						},
						{
							Type:       addonv1beta1.KubeClient,
							KubeClient: &addonv1beta1.KubeClientConfig{Driver: "csr"}, // valid
						},
						{
							Type: addonv1beta1.CustomSigner,
							CustomSigner: &addonv1beta1.CustomSignerConfig{
								SignerName: "", // invalid
							},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(
					addOnName, addOnNamespace, certificates.KubeAPIServerClientSignerName,
					csr.DefaultCommonName(testinghelpers.TestManagedClusterName, addOnName), "csr",
					addonv1beta1.KubeClient,
					[]string{csr.DefaultOrganization(testinghelpers.TestManagedClusterName, addOnName)}, nil, false),
			},
		},
		{
			name: "CustomSigner with default subject",
			addon: &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testinghelpers.TestManagedClusterName,
					Name:      addOnName,
				},
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					Registrations: []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.CustomSigner,
							CustomSigner: &addonv1beta1.CustomSignerConfig{
								SignerName: "custom.signer.io/test",
							},
						},
					},
					Namespace: addOnNamespace,
				},
			},
			configs: []registrationConfig{
				newRegistrationConfig(addOnName, addOnNamespace, "custom.signer.io/test",
					csr.DefaultCommonName(testinghelpers.TestManagedClusterName, addOnName), "",
					addonv1beta1.CustomSigner, nil, nil, false),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			installOption := addonInstallOption{
				AgentRunningOutsideManagedCluster: isAddonRunningOutsideManagedCluster(c.addon),
				InstallationNamespace:             getAddOnInstallationNamespace(c.addon),
			}
			logger := klog.NewKlogr()
			configs, err := getRegistrationConfigs(
				c.addon.Name, testinghelpers.TestManagedClusterName, installOption, c.addon.Status.Registrations, logger)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(configs) != len(c.configs) {
				t.Errorf("expected %d configs, but got %d", len(c.configs), len(configs))
			}

			for _, config := range c.configs {
				if _, ok := configs[config.hash]; !ok {
					t.Errorf("unexpected registrationConfig: %v, got %v", dump.Pretty(configs), dump.Pretty(c.configs))
				}
			}
		})
	}
}

func newRegistrationConfig(
	addOnName, addOnNamespace, signerName, commonName, driver string,
	registrationType addonv1beta1.RegistrationType,
	groups, organization []string,
	addOnAgentRunningOutsideManagedCluster bool) registrationConfig {

	registration := addonv1beta1.RegistrationConfig{
		Type: registrationType,
	}
	var secretName string
	switch registrationType {
	case addonv1beta1.KubeClient:
		registration.KubeClient = &addonv1beta1.KubeClientConfig{
			Driver: driver,
			Subject: addonv1beta1.KubeClientSubject{
				BaseSubject: addonv1beta1.BaseSubject{
					User:   commonName,
					Groups: groups,
				},
			},
		}
	case addonv1beta1.CustomSigner:
		registration.CustomSigner = &addonv1beta1.CustomSignerConfig{
			SignerName: signerName,
			Subject: addonv1beta1.Subject{
				BaseSubject: addonv1beta1.BaseSubject{
					User:   commonName,
					Groups: groups,
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
		secretName:   secretName,
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

func TestSetSubjectForRegistration(t *testing.T) {
	clusterName := testinghelpers.TestManagedClusterName
	addonName := "test-addon"

	cases := []struct {
		name           string
		registration   addonv1beta1.RegistrationConfig
		expectedUser   string
		expectedGroups []string
	}{
		{
			name: "KubeClient with csr driver - empty subject",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
				},
			},
			expectedUser:   csr.DefaultCommonName(clusterName, addonName),
			expectedGroups: []string{csr.DefaultOrganization(clusterName, addonName)},
		},
		{
			name: "KubeClient with csr driver - user set, groups empty",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User: "custom-user",
						},
					},
				},
			},
			expectedUser:   "custom-user",
			expectedGroups: []string{csr.DefaultOrganization(clusterName, addonName)},
		},
		{
			name: "KubeClient with csr driver - groups set, user empty",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							Groups: []string{"custom-group"},
						},
					},
				},
			},
			expectedUser:   csr.DefaultCommonName(clusterName, addonName),
			expectedGroups: []string{"custom-group"},
		},
		{
			name: "KubeClient with csr driver - both user and groups set",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "csr",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "custom-user",
							Groups: []string{"custom-group"},
						},
					},
				},
			},
			expectedUser:   "custom-user",
			expectedGroups: []string{"custom-group"},
		},
		{
			name: "KubeClient with token driver - empty subject",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "token",
				},
			},
			expectedUser: token.TokenSubject(clusterName, addonName).User,
		},
		{
			name: "KubeClient with token driver - user already set",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "token",
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User: "custom-user",
						},
					},
				},
			},
			expectedUser: token.TokenSubject(clusterName, addonName).User,
		},
		{
			name: "KubeClient with nil config",
			registration: addonv1beta1.RegistrationConfig{
				Type:       addonv1beta1.KubeClient,
				KubeClient: nil,
			},
			expectedUser: "",
		},
		{
			name: "CustomSigner - empty subject",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/test",
				},
			},
			expectedUser: csr.DefaultCommonName(clusterName, addonName),
		},
		{
			name: "CustomSigner - user already set",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CustomSigner,
				CustomSigner: &addonv1beta1.CustomSignerConfig{
					SignerName: "custom.signer.io/test",
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User: "custom-user",
						},
					},
				},
			},
			expectedUser: "custom-user",
		},
		{
			name: "CustomSigner with nil config",
			registration: addonv1beta1.RegistrationConfig{
				Type:         addonv1beta1.CustomSigner,
				CustomSigner: nil,
			},
			expectedUser: "",
		},
		{
			name: "KubeClient with unknown driver - no defaults set",
			registration: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Driver: "unknown",
				},
			},
			expectedUser: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := setSubjectForRegistration(addonName, clusterName, c.registration)

			var actualUser string
			var actualGroups []string

			switch result.Type {
			case addonv1beta1.KubeClient:
				if result.KubeClient != nil {
					actualUser = result.KubeClient.Subject.User
					actualGroups = result.KubeClient.Subject.Groups
				}
			case addonv1beta1.CustomSigner:
				if result.CustomSigner != nil {
					actualUser = result.CustomSigner.Subject.User
					actualGroups = result.CustomSigner.Subject.Groups
				}
			}

			if actualUser != c.expectedUser {
				t.Errorf("expected user %s, got %s", c.expectedUser, actualUser)
			}

			if len(c.expectedGroups) > 0 {
				if len(actualGroups) != len(c.expectedGroups) {
					t.Errorf("expected groups %v, got %v", c.expectedGroups, actualGroups)
				} else {
					for i, group := range c.expectedGroups {
						if actualGroups[i] != group {
							t.Errorf("expected groups %v, got %v", c.expectedGroups, actualGroups)
							break
						}
					}
				}
			}
		})
	}
}
