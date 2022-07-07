package registration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type testAgent struct {
	name          string
	registrations []addonapiv1alpha1.RegistrationConfig
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return []runtime.Object{}, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	if len(t.registrations) == 0 {
		return agent.AgentAddonOptions{
			AddonName: t.name,
		}
	}
	return agent.AgentAddonOptions{
		AddonName: t.name,
		Registration: &agent.RegistrationOption{
			CSRConfigurations: func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
				return t.registrations
			},
			PermissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
				return nil
			},
		},
	}
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		addon                []runtime.Object
		cluster              []runtime.Object
		testaddon            *testAgent
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                 "no cluster",
			addon:                []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster:              []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", registrations: []addonapiv1alpha1.RegistrationConfig{}},
		},
		{
			name:                 "no addon",
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", registrations: []addonapiv1alpha1.RegistrationConfig{}},
		},
		{
			name:                 "no registrations",
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon:                []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", registrations: []addonapiv1alpha1.RegistrationConfig{}},
		},
		{
			name:    "with registrations",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.InstallNamespace = "default"
					return addon
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.Registrations[0].SignerName != "test" {
					t.Errorf("Registration config is not updated")
				}
				if meta.IsStatusConditionFalse(addOn.Status.Conditions, "RegistrationApplied") {
					t.Errorf("addon condition is not correct: %v", addOn.Status.Conditions)
				}
			},
			testaddon: &testAgent{name: "test", registrations: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "test",
				},
			}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			for _, obj := range c.cluster {
				if err := clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.addon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := addonConfigurationController{
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
			}

			for _, obj := range c.addon {
				addon := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
				key := fmt.Sprintf("%s/%s", addon.Namespace, addon.Name)
				syncContext := addontesting.NewFakeSyncContext(t)
				err := controller.sync(context.TODO(), syncContext, key)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
			}

		})
	}
}
