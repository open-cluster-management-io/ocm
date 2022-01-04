package addonhealthcheck

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type testAgent struct {
	name   string
	health *agent.HealthProber
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return nil, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:    t.name,
		HealthProber: t.health,
	}
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		addon                []runtime.Object
		testaddon            *testAgent
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:  "no op if health checker is nil",
			addon: []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
			testaddon: &testAgent{
				name:   "test",
				health: nil,
			},
		},
		{
			name:  "update addon health check mode",
			addon: []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.HealthCheck.Mode != addonapiv1alpha1.HealthCheckModeCustomized {
					t.Errorf("Health check mode is not correct, expected %s but got %s",
						addonapiv1alpha1.HealthCheckModeCustomized, addOn.Status.HealthCheck.Mode)
				}
			},
			testaddon: &testAgent{
				name: "test",
				health: &agent.HealthProber{
					Type: agent.HealthProberTypeNone,
				},
			},
		},
		{
			name:  "no op if health checker mode is identical (None)",
			addon: []runtime.Object{NewAddonWithHealthCheck("test", "cluster1", addonapiv1alpha1.HealthCheckModeCustomized)},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
			testaddon: &testAgent{
				name: "test",
				health: &agent.HealthProber{
					Type: agent.HealthProberTypeNone,
				},
			},
		},
		{
			name:  "no op if health checker mode is identical (Lease)",
			addon: []runtime.Object{NewAddonWithHealthCheck("test", "cluster1", addonapiv1alpha1.HealthCheckModeLease)},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
			testaddon: &testAgent{
				name: "test",
				health: &agent.HealthProber{
					Type: agent.HealthProberTypeLease,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

			for _, obj := range c.addon {
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj)
			}

			controller := addonHealthCheckController{
				addonClient:               fakeAddonClient,
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:             eventstesting.NewTestingEventRecorder(t),
			}

			for _, addon := range c.addon {
				key, _ := cache.MetaNamespaceKeyFunc(addon)
				syncContext := addontesting.NewFakeSyncContext(t, key)
				err := controller.sync(context.TODO(), syncContext)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
			}

		})
	}
}

func NewAddonWithHealthCheck(name, namespace string, mode addonapiv1alpha1.HealthCheckMode) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, namespace)
	addon.Status.HealthCheck = addonapiv1alpha1.HealthCheck{Mode: mode}
	return addon
}
