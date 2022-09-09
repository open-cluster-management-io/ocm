package agentdeploy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/pointer"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/common/workapplier"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestDefaultHookReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		key                  string
		existingWork         []runtime.Object
		addon                []runtime.Object
		testaddon            *testAgent
		cluster              []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
		validateWorkActions  func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:    "deploy hook manifest for a created addon, add finalizer",
			key:     "cluster1/test",
			addon:   []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHookJob("default", "test")}},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				deployWork := actual.(*workapiv1.ManifestWork)
				if deployWork.Namespace != "cluster1" || deployWork.Name != constants.DeployWorkName("test") {
					t.Errorf("the deployWork %v/%v is incorrect.", deployWork.Namespace, deployWork.Name)
				}
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if !addonHasFinalizer(addOn, constants.PreDeleteHookFinalizer) {
					t.Errorf("the preDeleteHookFinalizer should be added.")
				}
			},
		},
		{
			name: "deploy hook manifest for a deleting addon with finalizer, not completed",
			key:  "cluster1/test",
			addon: []runtime.Object{
				func() runtime.Object {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.SetFinalizers([]string{constants.PreDeleteHookFinalizer})
					addon.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					return addon
				}(),
			},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHookJob("default", "test")}},
			existingWork: []runtime.Object{},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				hookWork := actual.(*workapiv1.ManifestWork)
				if hookWork.Namespace != "cluster1" || hookWork.Name != constants.PreDeleteHookWorkName("test") {
					t.Errorf("the hookWork %v/%v is not the hook job.", hookWork.Namespace, hookWork.Name)
				}
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionFalse(addOn.Status.Conditions, constants.AddonHookManifestCompleted) {
					t.Errorf("HookManifestCompleted condition should be false,but got true.")
				}
			},
		},
		{
			name: "deploy hook manifest for a deleting addon with finalizer, completed",
			key:  "cluster1/test",
			addon: []runtime.Object{
				addontesting.SetAddonFinalizers(
					addontesting.SetAddonDeletionTimestamp(addontesting.NewAddon("test", "cluster1"), time.Now()),
					constants.PreDeleteHookFinalizer),
			},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHookJob("test", "default")}},
			existingWork: []runtime.Object{
				func() *workapiv1.ManifestWork {
					work := addontesting.NewManifestWork(
						constants.PreDeleteHookWorkName("test"),
						"cluster1",
						addontesting.NewHookJob("test", "default"),
					)
					work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
						{
							ResourceIdentifier: workapiv1.ResourceIdentifier{
								Group:     "batch",
								Resource:  "jobs",
								Name:      "test",
								Namespace: "default",
							},
							FeedbackRules: []workapiv1.FeedbackRule{
								{
									Type: workapiv1.WellKnownStatusType,
								},
							},
						},
					}
					work.Status.Conditions = []metav1.Condition{
						{
							Type:   workapiv1.WorkApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   workapiv1.WorkAvailable,
							Status: metav1.ConditionTrue,
						},
					}
					work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
						Manifests: []workapiv1.ManifestCondition{
							{
								ResourceMeta: workapiv1.ManifestResourceMeta{
									Group:     "batch",
									Version:   "v1",
									Resource:  "jobs",
									Name:      "test",
									Namespace: "default",
								},
								StatusFeedbacks: workapiv1.StatusFeedbackResult{
									Values: []workapiv1.FeedbackValue{
										{
											Name: "JobComplete",
											Value: workapiv1.FieldValue{
												Type:   workapiv1.String,
												String: pointer.StringPtr("True"),
											},
										},
									},
								},
							},
						},
					}
					return work
				}(),
			},
			validateWorkActions: addontesting.AssertNoActions,
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// delete finalizer, patch completed condition.
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addonHasFinalizer(addOn, constants.PreDeleteHookFinalizer) {
					t.Errorf("expected no pre delete hook finalizer on managedCluster.")
				}
			},
		},
		{
			name: "deploy hook manifest for a deleting addon without finalizer, completed",
			key:  "cluster1/test",
			addon: []runtime.Object{
				addontesting.SetAddonDeletionTimestamp(addontesting.NewAddon("test", "cluster1"), time.Now())},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHookJob("test", "default")}},
			existingWork: []runtime.Object{
				func() *workapiv1.ManifestWork {
					work := addontesting.NewManifestWork(
						constants.PreDeleteHookWorkName("test"),
						"cluster1",
						addontesting.NewHookJob("test", "default"),
					)
					work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
						{
							ResourceIdentifier: workapiv1.ResourceIdentifier{
								Group:     "batch",
								Resource:  "jobs",
								Name:      "test",
								Namespace: "default",
							},
							FeedbackRules: []workapiv1.FeedbackRule{
								{
									Type: workapiv1.WellKnownStatusType,
								},
							},
						},
					}
					work.Status.Conditions = []metav1.Condition{
						{
							Type:   workapiv1.WorkApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   workapiv1.WorkAvailable,
							Status: metav1.ConditionTrue,
						},
					}
					work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
						Manifests: []workapiv1.ManifestCondition{
							{
								ResourceMeta: workapiv1.ManifestResourceMeta{
									Group:     "batch",
									Version:   "v1",
									Resource:  "jobs",
									Name:      "test",
									Namespace: "default",
								},
								StatusFeedbacks: workapiv1.StatusFeedbackResult{
									Values: []workapiv1.FeedbackValue{
										{
											Name: "JobComplete",
											Value: workapiv1.FieldValue{
												Type:   workapiv1.String,
												String: pointer.StringPtr("True"),
											},
										},
									},
								},
							},
						},
					}
					return work
				}(),
			},
			validateWorkActions: addontesting.AssertNoActions,
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// add finalizer
				addontesting.AssertActions(t, actions, "update")
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

			err := workInformerFactory.Work().V1().ManifestWorks().Informer().AddIndexers(
				cache.Indexers{
					byAddon:           indexByAddon,
					byHostedAddon:     indexByHostedAddon,
					hookByHostedAddon: indexHookByHostedAddon,
				},
			)

			if err != nil {
				t.Fatal(err)
			}

			for _, obj := range c.cluster {
				if err := clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj); err != nil {
					t.Error("failed to add cluster object to informer:", err)
				}
			}
			for _, obj := range c.addon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Errorf("failed to add addon object to informer: %v", err)
				}
			}
			for _, obj := range c.existingWork {
				if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(obj); err != nil {
					t.Errorf("failed to add work object to informer: %v", err)
				}
			}

			controller := addonDeployController{
				workApplier:               workapplier.NewWorkApplierWithTypedClient(fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister()),
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				workIndexer:               workInformerFactory.Work().V1().ManifestWorks().Informer().GetIndexer(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
			}

			syncContext := addontesting.NewFakeSyncContext(t)
			err = controller.sync(context.TODO(), syncContext, c.key)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
			c.validateWorkActions(t, fakeWorkClient.Actions())

		})
	}
}
