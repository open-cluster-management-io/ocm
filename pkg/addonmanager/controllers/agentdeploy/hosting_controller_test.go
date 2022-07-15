package agentdeploy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type testHostedAgent struct {
	name    string
	objects []runtime.Object
	err     error
}

func (t *testHostedAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (
	[]runtime.Object, error) {
	return t.objects, t.err
}

func (t *testHostedAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:         t.name,
		HostedModeEnabled: true,
	}
}

func TestHostingReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		existingWork         []runtime.Object
		addon                []runtime.Object
		testaddon            *testHostedAgent
		cluster              []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
		validateWorkActions  func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                 "no cluster",
			addon:                []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2")},
			cluster:              []runtime.Object{},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name:                 "no managed cluster",
			addon:                []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2")},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster2")},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name:         "no hosting cluster",
			addon:        []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2")},
			cluster:      []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			existingWork: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// Update addon condition
				addontesting.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, constants.HostingClusterValidity)
				if addOnCond == nil {
					t.Fatal("condition should not be nil")
				}
				if addOnCond.Reason != constants.HostingClusterValidityReasonInvalid {
					t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
				}
			},
			validateWorkActions: addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name:  "add finalizer",
			addon: []runtime.Object{addontesting.NewHostedModeAddon("test", "cluster1", "cluster2")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			existingWork: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// Update addon condition
				addontesting.AssertActions(t, actions, "update")
				update := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := update.(*addonapiv1alpha1.ManagedClusterAddOn)
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, constants.HostingClusterValidity)
				if addOnCond == nil {
					t.Fatal("condition should not be nil")
				}
				if addOnCond.Reason != constants.HostingClusterValidityReasonValid {
					t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
				}

				if len(addOn.Finalizers) != 1 {
					t.Errorf("expected 1 finalizer, but got %v", len(addOn.Finalizers))
				}
				if !hasFinalizer(addOn.Finalizers, constants.HostingManifestFinalizer) {
					t.Errorf("expected hosting manifest finalizer")
				}
			},
			validateWorkActions: addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name:                 "no addon",
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name:                 "not hosted mode addon",
			addon:                []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
			}},
		},
		{
			name:  "deploy manifests for an addon",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
			}},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, constants.AddonHostingManifestApplied)
				if addOnCond == nil {
					t.Fatal("condition should not be nil")
				}
				if addOnCond.Reason != constants.AddonManifestAppliedReasonManifestsApplyFailed {
					t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
				}
			},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
			},
		},
		{
			name:  "update manifest for an addon",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
			}},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkName("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test1"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if meta.IsStatusConditionFalse(addOn.Status.Conditions, constants.AddonHostingManifestApplied) {
					t.Errorf("Condition Reason is not correct: %v", addOn.Status.Conditions)
				}
			},
		},
		{
			name:  "do not update manifest for an addon",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
			}},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkName("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
				)
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: addontesting.AssertNoActions,
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if meta.IsStatusConditionFalse(addOn.Status.Conditions, constants.AddonHostingManifestApplied) {
					t.Errorf("Condition Reason is not correct: %v", addOn.Status.Conditions)
				}
			},
		},
		{
			name:  "get error when run manifest from agent",
			addon: []runtime.Object{addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2")},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{
				name: "test",
				objects: []runtime.Object{
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				},
				err: fmt.Errorf("run manifest failed"),
			},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkName("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
				)
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: addontesting.AssertNoActions,
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")

				assertHostingClusterValid(t, actions[0])

				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionFalse(addOn.Status.Conditions, constants.AddonHostingManifestApplied) {
					t.Errorf("Condition Reason is not correct: %v", addOn.Status.Conditions)
				}
			},
		},
		{
			name: "delete finalizer",
			addon: []runtime.Object{addontesting.SetAddonDeletionTimestamp(
				addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2"),
				time.Now(),
			)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
			}},
			existingWork: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					constants.DeployHostingWorkName("cluster1", "test"),
					"cluster2",
					addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
					addontesting.NewHostingUnstructured("v1", "Deployment", "default", "test"),
				)
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "delete")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				update := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := update.(*addonapiv1alpha1.ManagedClusterAddOn)
				if hasFinalizer(addOn.Finalizers, constants.HostingManifestFinalizer) {
					t.Errorf("expected hosting manifest finalizer")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeWorkClient := fakework.NewSimpleClientset(c.existingWork...)
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addon...)

			workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
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
			for _, obj := range c.existingWork {
				if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := addonHostingDeployController{
				workClient:                fakeWorkClient,
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				workLister:                workInformerFactory.Work().V1().ManifestWorks().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				cache:                     newWorkCache(),
			}

			for _, obj := range c.addon {
				addon := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
				key := fmt.Sprintf("%s/%s", addon.Namespace, addon.Name)
				syncContext := addontesting.NewFakeSyncContext(t)
				err := controller.sync(context.TODO(), syncContext, key)
				if err != c.testaddon.err {
					t.Errorf("test case %s: expected error %v when sync got %v", c.name, c.testaddon.err, err)
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
				c.validateWorkActions(t, fakeWorkClient.Actions())
			}

		})
	}
}

func assertHostingClusterValid(t *testing.T, actions clienttesting.Action) {
	patch := actions.(clienttesting.PatchActionImpl).Patch
	addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
	err := json.Unmarshal(patch, addOn)
	if err != nil {
		t.Fatal(err)
	}
	addOnCond := meta.FindStatusCondition(addOn.Status.Conditions, constants.HostingClusterValidity)
	if addOnCond == nil {
		t.Fatal("condition should not be nil")
	}
	if addOnCond.Reason != constants.HostingClusterValidityReasonValid {
		t.Errorf("Condition Reason is not correct: %v", addOnCond.Reason)
	}
}
