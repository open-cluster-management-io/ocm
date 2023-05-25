package addon

import (
	"context"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestFilterCSREvents(t *testing.T) {
	clusterName := "cluster1"
	addonName := "addon1"
	signerName := "signer1"

	cases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected bool
	}{
		{
			name: "csr not from the managed cluster",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr not for the addon",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr with different signer name",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "valid csr",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						// the labels are only hints. Anyone could set/modify them.
						clusterv1.ClusterNameLabelKey: clusterName,
						addonv1alpha1.AddonLabelKey:   addonName,
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: signerName,
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filterFunc := createCSREventFilterFunc(clusterName, addonName, signerName)
			actual := filterFunc(c.csr)
			if actual != c.expected {
				t.Errorf("Expected %v but got %v", c.expected, actual)
			}
		})
	}
}

func TestRegistrationSync(t *testing.T) {
	clusterName := "cluster1"
	addonName := "addon1"
	signerName := "signer1"

	config1 := addonv1alpha1.RegistrationConfig{
		SignerName: signerName,
	}

	config2 := addonv1alpha1.RegistrationConfig{
		SignerName: signerName,
		Subject: addonv1alpha1.Subject{
			User: addonName,
		},
	}

	cases := []struct {
		name                                 string
		queueKey                             string
		addOn                                *addonv1alpha1.ManagedClusterAddOn
		addOnRegistrationConfigs             map[string]map[string]registrationConfig
		addonAgentOutsideManagedCluster      bool
		expectedAddOnRegistrationConfigHashs map[string][]string
		validateActions                      func(t *testing.T, actions, managementActions []clienttesting.Action)
	}{
		{
			name:     "addon registration not enabled",
			queueKey: addonName,
			addOn:    newManagedClusterAddOn(clusterName, addonName, nil, false),
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
				if len(managementActions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
			},
		},
		{
			name:     "addon registration enabled",
			queueKey: addonName,
			addOn: newManagedClusterAddOn(clusterName, addonName,
				[]addonv1alpha1.RegistrationConfig{config1}, false),
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config1, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
			},
		},
		{
			name:     "addon registration updated",
			queueKey: addonName,
			addOn: newManagedClusterAddOn(clusterName, addonName,
				[]addonv1alpha1.RegistrationConfig{config2}, false),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addonName,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config2, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "addon install namespace updated",
			queueKey: addonName,
			addOn: setAddonInstallNamespace(newManagedClusterAddOn(clusterName, addonName,
				[]addonv1alpha1.RegistrationConfig{config2}, false), "ns1"),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config2, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addonName,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config2, "ns1", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "addon is deleted",
			queueKey: addonName,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addonName,
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "hosted addon registration enabled",
			queueKey: addonName,
			addOn:    newManagedClusterAddOn(clusterName, addonName, []addonv1alpha1.RegistrationConfig{config1}, true),
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config1, "", true)},
			},
			addonAgentOutsideManagedCluster: true,
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
				if len(managementActions) != 0 {
					t.Errorf("expect 0 management actions but got %d", len(managementActions))
				}
			},
		},
		{
			name:     "hosted addon registration updated",
			queueKey: addonName,
			addOn: newManagedClusterAddOn(clusterName, addonName,
				[]addonv1alpha1.RegistrationConfig{config2}, true),
			addonAgentOutsideManagedCluster: true,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1, "", true): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace:             addonName,
							AgentRunningOutsideManagedCluster: true,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config2, "", true)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
				if len(managementActions) != 1 {
					t.Errorf("expect 1 management actions but got %d", len(managementActions))
				}
				testinghelpers.AssertActions(t, managementActions, "delete")
			},
		},
		{
			name:     "deploy mode changes from hosted to default",
			queueKey: addonName,
			addOn: newManagedClusterAddOn(clusterName, addonName,
				[]addonv1alpha1.RegistrationConfig{config2}, false),
			addonAgentOutsideManagedCluster: false,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config2, "", true): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							AgentRunningOutsideManagedCluster: true,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config2, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
				if len(managementActions) != 1 {
					t.Errorf("expect 1 management actions but got %d", len(managementActions))
				}
				testinghelpers.AssertActions(t, managementActions, "delete")
			},
		},
		{
			name:     "deploy mode changes from default to hosted",
			queueKey: addonName,
			addOn: newManagedClusterAddOn(clusterName, addonName,
				[]addonv1alpha1.RegistrationConfig{config2}, true),
			addonAgentOutsideManagedCluster: true,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config2, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace:             addonName,
							AgentRunningOutsideManagedCluster: false,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config2, "", true)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(managementActions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(managementActions))
				}
				if len(actions) != 1 {
					t.Errorf("expect 1 management actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "hosted addon is deleted",
			queueKey: addonName,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1, "", true): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace:             addonName,
							AgentRunningOutsideManagedCluster: true,
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(managementActions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(managementActions))
				}
				testinghelpers.AssertActions(t, managementActions, "delete")
			},
		},
		{
			name:     "resync",
			queueKey: factory.DefaultQueueKey,
			addOn: newManagedClusterAddOn(clusterName, addonName,
				[]addonv1alpha1.RegistrationConfig{config1}, false),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addonName,
						},
					},
				},
				"addon2": {
					hash(config1, "", false): {
						secretName: "secret2",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: "addon2",
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config1, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset()
			managementClient := kubefake.NewSimpleClientset()
			addons := []runtime.Object{}
			if c.addOn != nil {
				addons = append(addons, c.addOn)
			}
			addonClient := addonfake.NewSimpleClientset(addons...)
			addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, time.Minute*10)
			addonStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
			if c.addOn != nil {
				if err := addonStore.Add(c.addOn); err != nil {
					t.Fatal(err)
				}
			}

			if c.addOnRegistrationConfigs == nil {
				c.addOnRegistrationConfigs = map[string]map[string]registrationConfig{}
			}

			controller := addOnRegistrationController{
				clusterName:          clusterName,
				managementKubeClient: managementClient,
				spokeKubeClient:      kubeClient,
				hubAddOnLister:       addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				recorder:             eventstesting.NewTestingEventRecorder(t),
				startRegistrationFunc: func(ctx context.Context, config registrationConfig) context.CancelFunc {
					_, cancel := context.WithCancel(context.Background())
					return cancel
				},
				addOnRegistrationConfigs: c.addOnRegistrationConfigs,
			}

			err := controller.sync(context.Background(), testinghelpers.NewFakeSyncContext(t, c.queueKey))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if len(c.expectedAddOnRegistrationConfigHashs) != len(controller.addOnRegistrationConfigs) {
				t.Errorf("expected %d addOns, but got %d",
					len(c.expectedAddOnRegistrationConfigHashs), len(controller.addOnRegistrationConfigs))
			}

			for addOnName, hashs := range c.expectedAddOnRegistrationConfigHashs {
				addonRegistrationConfigs := controller.addOnRegistrationConfigs[addOnName]
				if len(addonRegistrationConfigs) != len(hashs) {
					t.Errorf("expected %d config items for addOn %q, but got %d",
						len(hashs), addOnName, len(addonRegistrationConfigs))
				}
				for _, hash := range hashs {
					config, ok := addonRegistrationConfigs[hash]
					if !ok {
						t.Errorf("registration config with hash %q is not found for addOn %q", hash, addOnName)

					}
					if config.AgentRunningOutsideManagedCluster != c.addonAgentOutsideManagedCluster {
						t.Errorf("expect addon agent running outside managed cluster: %v, but got: %v",
							c.addonAgentOutsideManagedCluster, config.AgentRunningOutsideManagedCluster)
					}
				}
			}

			if c.validateActions != nil {
				c.validateActions(t, kubeClient.Actions(), managementClient.Actions())
			}
		})
	}
}

func newManagedClusterAddOn(namespace, name string, registrations []addonv1alpha1.RegistrationConfig,
	hostedMode bool) *addonv1alpha1.ManagedClusterAddOn {
	addon := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: addonv1alpha1.ManagedClusterAddOnStatus{
			Registrations: registrations,
		},
	}

	if hostedMode {
		addon.SetAnnotations(map[string]string{hostingClusterNameAnnotation: "test"})
	}
	return addon
}

func setAddonInstallNamespace(
	addon *addonv1alpha1.ManagedClusterAddOn,
	namespace string) *addonv1alpha1.ManagedClusterAddOn {
	addon.Spec.InstallNamespace = namespace
	return addon
}

func hash(registration addonv1alpha1.RegistrationConfig, installNamespace string,
	addOnAgentRunningOutsideManagedCluster bool) string {
	if len(installNamespace) == 0 {
		installNamespace = defaultAddOnInstallationNamespace
	}

	h, _ := getConfigHash(registration, addonInstallOption{
		InstallationNamespace:             installNamespace,
		AgentRunningOutsideManagedCluster: addOnAgentRunningOutsideManagedCluster,
	})
	return h
}
