package addon

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
	registertesting "open-cluster-management.io/ocm/pkg/registration/register/testing"
)

type testDriverFactory struct{}

func (f *testDriverFactory) Fork(addonName string, authConfig register.AddonAuthConfig, secretOption register.SecretOption) (register.RegisterDriver, error) {
	return nil, nil
}

func TestRegistrationSync(t *testing.T) {
	clusterName := "cluster1"
	signerName := "signer1"

	config1 := addonv1alpha1.RegistrationConfig{
		SignerName: signerName,
	}

	config2 := addonv1alpha1.RegistrationConfig{
		SignerName: signerName,
		Subject: addonv1alpha1.Subject{
			User: addOnName,
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
			queueKey: addOnName,
			addOn:    newManagedClusterAddOn(clusterName, addOnName, nil, false),
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
			queueKey: addOnName,
			addOn: newManagedClusterAddOn(clusterName, addOnName,
				[]addonv1alpha1.RegistrationConfig{config1}, false),
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addOnName: {hash(config1, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
			},
		},
		{
			name:     "addon registration updated",
			queueKey: addOnName,
			addOn: newManagedClusterAddOn(clusterName, addOnName,
				[]addonv1alpha1.RegistrationConfig{config2}, false),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config1, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addOnName,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addOnName: {hash(config2, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "addon install namespace updated",
			queueKey: addOnName,
			addOn: setAddonInstallNamespace(newManagedClusterAddOn(clusterName, addOnName,
				[]addonv1alpha1.RegistrationConfig{config2}, false), "ns1"),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config2, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addOnName,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addOnName: {hash(config2, "ns1", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "addon is deleted",
			queueKey: addOnName,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config1, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addOnName,
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "hosted addon registration enabled",
			queueKey: addOnName,
			addOn:    newManagedClusterAddOn(clusterName, addOnName, []addonv1alpha1.RegistrationConfig{config1}, true),
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addOnName: {hash(config1, "", true)},
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
			queueKey: addOnName,
			addOn: newManagedClusterAddOn(clusterName, addOnName,
				[]addonv1alpha1.RegistrationConfig{config2}, true),
			addonAgentOutsideManagedCluster: true,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config1, "", true): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace:             addOnName,
							AgentRunningOutsideManagedCluster: true,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addOnName: {hash(config2, "", true)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
				if len(managementActions) != 1 {
					t.Errorf("expect 1 management actions but got %d", len(managementActions))
				}
				testingcommon.AssertActions(t, managementActions, "delete")
			},
		},
		{
			name:     "deploy mode changes from hosted to default",
			queueKey: addOnName,
			addOn: newManagedClusterAddOn(clusterName, addOnName,
				[]addonv1alpha1.RegistrationConfig{config2}, false),
			addonAgentOutsideManagedCluster: false,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config2, "", true): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							AgentRunningOutsideManagedCluster: true,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addOnName: {hash(config2, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
				if len(managementActions) != 1 {
					t.Errorf("expect 1 management actions but got %d", len(managementActions))
				}
				testingcommon.AssertActions(t, managementActions, "delete")
			},
		},
		{
			name:     "deploy mode changes from default to hosted",
			queueKey: addOnName,
			addOn: newManagedClusterAddOn(clusterName, addOnName,
				[]addonv1alpha1.RegistrationConfig{config2}, true),
			addonAgentOutsideManagedCluster: true,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config2, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace:             addOnName,
							AgentRunningOutsideManagedCluster: false,
						},
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addOnName: {hash(config2, "", true)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(managementActions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(managementActions))
				}
				if len(actions) != 1 {
					t.Errorf("expect 1 management actions but got %d", len(actions))
				}
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "hosted addon is deleted",
			queueKey: addOnName,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config1, "", true): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace:             addOnName,
							AgentRunningOutsideManagedCluster: true,
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(managementActions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(managementActions))
				}
				testingcommon.AssertActions(t, managementActions, "delete")
			},
		},
		{
			name:     "resync",
			queueKey: factory.DefaultQueueKey,
			addOn: newManagedClusterAddOn(clusterName, addOnName,
				[]addonv1alpha1.RegistrationConfig{config1}, false),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addOnName: {
					hash(config1, "", false): {
						secretName: "secret1",
						addonInstallOption: addonInstallOption{
							InstallationNamespace: addOnName,
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
				addOnName: {hash(config1, "", false)},
			},
			validateActions: func(t *testing.T, actions, managementActions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewClientset()
			managementClient := kubefake.NewClientset()
			var addons []runtime.Object
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
				patcher: patcher.NewPatcher[
					*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus](
					addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName)),
				addonDriverFactory: &testDriverFactory{},
				startRegistrationFunc: func(ctx context.Context, config registrationConfig) (context.CancelFunc, error) {
					_, cancel := context.WithCancel(context.Background())
					return cancel, nil
				},
				addOnRegistrationConfigs: c.addOnRegistrationConfigs,
				addonAuthConfig: &registertesting.TestAddonAuthConfig{
					KubeClientAuth: "csr",
				},
			}

			// First sync: sets the driver and returns early (status updated)
			err := controller.sync(context.Background(), testingcommon.NewFakeSyncContext(t, c.queueKey), c.queueKey)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Second sync: processes registrations (only if addon has registrations to process)
			// The condition checks if there are registrations to test, not whether driver is set
			if c.addOn != nil && len(c.addOn.Status.Registrations) > 0 {
				// Update addon in store with driver set (simulating informer update)
				updatedAddOn := c.addOn.DeepCopy()
				updatedAddOn.Status.KubeClientDriver = "csr"
				if err := addonStore.Update(updatedAddOn); err != nil {
					t.Fatal(err)
				}

				// Sync again to process registrations
				err = controller.sync(context.Background(), testingcommon.NewFakeSyncContext(t, c.queueKey), c.queueKey)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
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
		addon.SetAnnotations(map[string]string{addonv1alpha1.HostingClusterNameAnnotationKey: "test"})
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
	}, "")
	return h
}
