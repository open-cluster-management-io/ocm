package clustermanagement

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
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
	name string
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return []runtime.Object{}, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: t.name,
	}
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		cluster                []runtime.Object
		testaddon              *testAgent
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
		queueLen               int
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKey:                "test/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			cluster:                []runtime.Object{},
			testaddon:              &testAgent{name: "test"},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                   "no cluster",
			syncKey:                "test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster:                []runtime.Object{},
			testaddon:              &testAgent{name: "test"},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                   "no managedclusteraddon",
			syncKey:                "test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster:                []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon:              &testAgent{name: "test"},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:    "queue managedclusteraddon",
			syncKey: "test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
				addontesting.NewManagedCluster("cluster3"),
			},
			testaddon:            &testAgent{name: "test"},
			validateAddonActions: addontesting.AssertNoActions,
			queueLen:             2,
		},
		{
			name:                   "no managedclusteraddon to sync",
			syncKey:                "cluster1/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon:            &testAgent{name: "test"},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                   "update managedclusteraddon",
			syncKey:                "cluster1/test",
			managedClusteraddon:    []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon: &testAgent{name: "test"},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.AddOnConfiguration.CRDName != "testcrd" || addOn.Status.AddOnConfiguration.CRName != "testcr" {
					t.Errorf("Config coordinate is not updated")
				}
			},
		},
		{
			name:                   "no need to update managedclusteraddon",
			syncKey:                "cluster1/test",
			managedClusteraddon:    []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
			},
			testaddon:            &testAgent{name: "test"},
			validateAddonActions: addontesting.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)

			obj := append(c.clusterManagementAddon, c.managedClusteraddon...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			for _, obj := range c.cluster {
				clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj)
			}
			for _, obj := range c.managedClusteraddon {
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj)
			}
			for _, obj := range c.clusterManagementAddon {
				addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj)
			}

			controller := clusterManagementController{
				addonClient:                  fakeAddonClient,
				managedClusterLister:         clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:                  map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:                eventstesting.NewTestingEventRecorder(t),
			}

			syncContext := addontesting.NewFakeSyncContext(t, c.syncKey)
			err := controller.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())

			if c.queueLen != syncContext.Queue().Len() {
				t.Errorf("Expect queue size is %d, but got %d", c.queueLen, syncContext.Queue().Len())
			}
		})
	}
}
