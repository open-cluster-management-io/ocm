package registration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/open-cluster-management/addon-framework/pkg/addonmanager/addontesting"
	"github.com/open-cluster-management/addon-framework/pkg/agent"
	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	fakeaddon "github.com/open-cluster-management/api/client/addon/clientset/versioned/fake"
	addoninformers "github.com/open-cluster-management/api/client/addon/informers/externalversions"
	fakecluster "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
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
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
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
				clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj)
			}
			for _, obj := range c.addon {
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj)
			}

			controller := addonConfigurationController{
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:             eventstesting.NewTestingEventRecorder(t),
			}

			for _, obj := range c.addon {
				addon := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
				syncContext := addontesting.NewFakeSyncContext(t, fmt.Sprintf("%s/%s", addon.Namespace, addon.Name))
				err := controller.sync(context.TODO(), syncContext)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
			}

		})
	}
}
