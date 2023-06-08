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
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	"open-cluster-management.io/api/utils/work/v1/workbuilder"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestHostingHookReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		key                  string
		existingWork         []runtime.Object
		addon                []runtime.Object
		testaddon            *testHostedAgent
		cluster              []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
		validateWorkActions  func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "deploy hook manifest for a created addon, add finalizer",
			key:  "cluster1/test",
			addon: []runtime.Object{
				addontesting.NewHostedModeAddonWithFinalizer("test", "cluster1", "cluster2",
					registrationAppliedCondition)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostedHookJob("test", "default"),
			}},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if !addonHasFinalizer(addOn, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer) {
					t.Errorf("the preDeleteHookFinalizer should be added.")
				}
			},
		},
		{
			name: "deploy hook manifest for a created addon with 2 finalizers",
			key:  "cluster1/test",
			addon: []runtime.Object{
				addontesting.SetAddonFinalizers(
					addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
						registrationAppliedCondition),
					addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer, addonapiv1alpha1.AddonHostingManifestFinalizer)},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostedHookJob("test", "default"),
			}},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
			},
		},
		{
			name: "deploy hook manifest for a deleting addon with finalizer, not completed",
			key:  "cluster1/test",
			addon: []runtime.Object{
				addontesting.SetAddonFinalizers(
					addontesting.SetAddonDeletionTimestamp(
						addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
							registrationAppliedCondition), time.Now()),
					addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer, addonapiv1alpha1.AddonHostingManifestFinalizer),
			},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostedHookJob("test", "default"),
			}},
			existingWork: []runtime.Object{},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				// hosted sync deploy the hook work in the hosting cluster ns
				addontesting.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				deployWork := actual.(*workapiv1.ManifestWork)
				if deployWork.Namespace != "cluster2" || deployWork.Name != constants.PreDeleteHookHostingWorkName("cluster1", "test") {
					t.Errorf("the hookWork %v/%v is not the hook job.", deployWork.Namespace, deployWork.Name)
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
				if !meta.IsStatusConditionFalse(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHookManifestCompleted) {
					t.Errorf("HookManifestCompleted condition should be false,but got true.")
				}
			},
		},
		{
			name: "deploy hook manifest for a deleting addon with 2 finalizer, completed",
			key:  "cluster1/test",
			addon: []runtime.Object{
				addontesting.SetAddonFinalizers(
					addontesting.SetAddonDeletionTimestamp(
						addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
							registrationAppliedCondition), time.Now()),
					addonapiv1alpha1.AddonHostingManifestFinalizer, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer),
			},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostedHookJob("test", "default"),
			}},
			existingWork: []runtime.Object{
				func() *workapiv1.ManifestWork {
					work := addontesting.NewManifestWork(
						constants.PreDeleteHookHostingWorkName("cluster1", "test"),
						"cluster2",
						addontesting.NewHostedHookJob("test", "default"),
					)
					work.Labels = map[string]string{
						addonapiv1alpha1.AddonLabelKey:          "test",
						addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
					}
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
												String: pointer.String("True"),
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
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				// hosted hook sync deletes the hook work in the hosting cluster ns
				addontesting.AssertActions(t, actions, "delete")
				if actions[0].(clienttesting.DeleteActionImpl).Name != constants.PreDeleteHookHostingWorkName("cluster1", "test") {
					t.Errorf("should delete the hook work after completed")
				}
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// delete HostingPreDeleteHookFinalizer firstly
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addonHasFinalizer(addOn, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer) {
					t.Errorf("expected no HostingManifestFinalizer on addon.")
				}
			},
		},
		{
			name: "deploy hook manifest for a deleting addon with 1 finalizer, completed",
			key:  "cluster1/test",
			addon: []runtime.Object{
				addontesting.SetAddonFinalizers(
					addontesting.SetAddonDeletionTimestamp(
						addontesting.NewHostedModeAddon("test", "cluster1", "cluster2",
							registrationAppliedCondition), time.Now()),
					addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer),
			},
			cluster: []runtime.Object{
				addontesting.NewManagedCluster("cluster1"),
				addontesting.NewManagedCluster("cluster2"),
			},
			testaddon: &testHostedAgent{name: "test", objects: []runtime.Object{
				addontesting.NewHostingUnstructured("v1", "ConfigMap", "default", "test"),
				addontesting.NewHostedHookJob("test", "default"),
			}},
			existingWork: []runtime.Object{
				func() *workapiv1.ManifestWork {
					work := addontesting.NewManifestWork(
						constants.PreDeleteHookHostingWorkName("cluster1", "test"),
						"cluster2",
						addontesting.NewHostedHookJob("test", "default"),
					)
					work.Labels = map[string]string{
						addonapiv1alpha1.AddonLabelKey:          "test",
						addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
					}
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
												String: pointer.String("True"),
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
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				// hosted sync deletes the hook work in the hosting cluster ns
				addontesting.AssertActions(t, actions, "delete")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				// delete HostingPreDeleteHookFinalizer
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addonHasFinalizer(addOn, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer) {
					t.Errorf("expected no HostingPreDeleteHookFinalizer on addon.")
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

			controller := addonDeployController{
				workApplier:               workapplier.NewWorkApplierWithTypedClient(fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister()),
				workBuilder:               workbuilder.NewWorkBuilder(),
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				workIndexer:               workInformerFactory.Work().V1().ManifestWorks().Informer().GetIndexer(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
			}

			syncContext := addontesting.NewFakeSyncContext(t)
			err = controller.sync(context.TODO(), syncContext, c.key)
			if (err == nil && c.testaddon.err != nil) || (err != nil && c.testaddon.err == nil) {
				t.Errorf("expected error %v when sync got %v", c.testaddon.err, err)
			}
			if err != nil && c.testaddon.err != nil && err.Error() != c.testaddon.err.Error() {
				t.Errorf("expected error %v when sync got %v", c.testaddon.err, err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
			c.validateWorkActions(t, fakeWorkClient.Actions())
		})
	}
}
