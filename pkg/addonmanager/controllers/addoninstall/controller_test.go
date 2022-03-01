package addoninstall

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	name     string
	strategy *agent.InstallStrategy
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return nil, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:       t.name,
		InstallStrategy: t.strategy,
	}
}

func newManagedClusterWithLabel(name, key, value string) *clusterv1.ManagedCluster {
	cluster := addontesting.NewManagedCluster(name)
	cluster.Labels = map[string]string{key: value}

	return cluster
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		addon                []runtime.Object
		testaddon            *testAgent
		cluster              []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                 "no install strategy",
			addon:                []runtime.Object{},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", strategy: nil},
		},
		{
			name:    "all install strategy",
			addon:   []runtime.Object{},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Spec.InstallNamespace != "test" {
					t.Errorf("Install namespace is not correct, expected test but got %s", addOn.Spec.InstallNamespace)
				}
			},
			testaddon: &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
		},
		{
			name:    "update addon with all install strategy",
			addon:   []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Spec.InstallNamespace != "test" {
					t.Errorf("Install namespace is not correct, expected test but got %s", addOn.Spec.InstallNamespace)
				}
			},
			testaddon: &testAgent{name: "test", strategy: agent.InstallAllStrategy("test")},
		},
		{
			name:                 "selector install strategy with unmatched cluster",
			addon:                []runtime.Object{},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon: &testAgent{name: "test", strategy: agent.InstallByLabelStrategy("test", metav1.LabelSelector{
				MatchLabels: map[string]string{"mode": "dev"},
			})},
		},
		{
			name:                 "selector install strategy with nil label selector",
			addon:                []runtime.Object{},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateAddonActions: addontesting.AssertNoActions,
			testaddon:            &testAgent{name: "test", strategy: &agent.InstallStrategy{Type: agent.InstallByLabel}},
		},
		{
			name:    "selector install strategy with matched cluster",
			addon:   []runtime.Object{},
			cluster: []runtime.Object{newManagedClusterWithLabel("cluster1", "mode", "dev")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Spec.InstallNamespace != "test" {
					t.Errorf("Install namespace is not correct, expected test but got %s", addOn.Spec.InstallNamespace)
				}
			},
			testaddon: &testAgent{name: "test", strategy: agent.InstallByLabelStrategy("test", metav1.LabelSelector{
				MatchLabels: map[string]string{"mode": "dev"},
			})},
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

			controller := addonInstallController{
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:             eventstesting.NewTestingEventRecorder(t),
			}

			for _, obj := range c.cluster {
				mc := obj.(*clusterv1.ManagedCluster)
				syncContext := addontesting.NewFakeSyncContext(t, mc.Name)
				err := controller.sync(context.TODO(), syncContext)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
			}

		})
	}
}
