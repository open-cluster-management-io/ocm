package agentdeploy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func newHookJob(name, namespace string) *unstructured.Unstructured {
	job := addontesting.NewUnstructured("batch/v1", "Job", namespace, name)
	job.SetLabels(map[string]string{constants.PreDeleteHookLabel: ""})
	return job
}

func newDeletingAddon(name, namespace string, finalizers ...string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, namespace)
	addon.SetFinalizers(finalizers)
	addon.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	return addon
}

func TestHookWorkReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		existingWork         []runtime.Object
		addon                []runtime.Object
		testaddon            *testAgent
		cluster              []runtime.Object
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
		validateWorkActions  func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                 "no cluster",
			addon:                []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster:              []runtime.Object{},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("batch/v1", "Job", "default", "test"),
			}},
		},
		{
			name:                 "no addon",
			cluster:              []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			existingWork:         []runtime.Object{},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
			testaddon: &testAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("batch/v1", "Job", "default", "test"),
			}},
		},
		{
			name:    "deploy manifests without hook for an addon",
			addon:   []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{
				addontesting.NewUnstructured("batch/v1", "Job", "default", "test"),
			}},
			validateAddonActions: addontesting.AssertNoActions,
			validateWorkActions:  addontesting.AssertNoActions,
		},
		{
			name:                "deploy hook manifest for a created addon",
			addon:               []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			cluster:             []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon:           &testAgent{name: "test", objects: []runtime.Object{newHookJob("default", "test")}},
			validateWorkActions: addontesting.AssertNoActions,
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if !hasFinalizer(addOn.Finalizers, constants.PreDeleteHookFinalizer) {
					t.Errorf("the preDeleteHookFinalizer should be added.")
				}
			},
		},
		{
			name: "deploy hook manifest for a deleting addon, not completed",
			addon: []runtime.Object{
				func() runtime.Object {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.SetFinalizers([]string{constants.PreDeleteHookFinalizer})
					addon.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					return addon
				}(),
			},
			cluster:   []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{newHookJob("default", "test")}},
			validateWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "create")
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if !meta.IsStatusConditionFalse(addOn.Status.Conditions, "HookManifestCompleted") {
					t.Errorf("HookManifestCompleted condition should be false,but got true.")
				}
			},
		},
		{
			name:      "deploy hook manifest for a deleting addon, has finalizer completed",
			addon:     []runtime.Object{newDeletingAddon("test", "cluster1", constants.PreDeleteHookFinalizer)},
			cluster:   []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{newHookJob("test", "default")}},
			existingWork: []runtime.Object{
				func() *workapiv1.ManifestWork {
					work := addontesting.NewManifestWork(
						preDeleteHookWorkName("test"),
						"cluster1",
						newHookJob("test", "default"),
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
												String: StringPtr("True"),
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
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if hasFinalizer(addOn.Finalizers, constants.PreDeleteHookFinalizer) {
					t.Errorf("expected no pre delete hoook finalizer.")
				}
			},
		},
		{
			name:      "deploy hook manifest for a deleting addon, no finalizer, completed",
			addon:     []runtime.Object{newDeletingAddon("test", "cluster1")},
			cluster:   []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			testaddon: &testAgent{name: "test", objects: []runtime.Object{newHookJob("test", "default")}},
			existingWork: []runtime.Object{
				func() *workapiv1.ManifestWork {
					work := addontesting.NewManifestWork(
						preDeleteHookWorkName("test"),
						"cluster1",
						newHookJob("test", "default"),
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
												String: StringPtr("True"),
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
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if !meta.IsStatusConditionTrue(addOn.Status.Conditions, "HookManifestCompleted") {
					t.Errorf("HookManifestCompleted condition should be true,but got false.")
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
				clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj)
			}
			for _, obj := range c.addon {
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj)
			}
			for _, obj := range c.existingWork {
				workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(obj)
			}

			controller := addonHookDeployController{
				workClient:                fakeWorkClient,
				addonClient:               fakeAddonClient,
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				workLister:                workInformerFactory.Work().V1().ManifestWorks().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:             eventstesting.NewTestingEventRecorder(t),
				cache:                     newWorkCache(),
			}

			for _, obj := range c.addon {
				addon := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
				syncContext := addontesting.NewFakeSyncContext(t, fmt.Sprintf("%s/%s", addon.Namespace, addon.Name))
				err := controller.sync(context.TODO(), syncContext)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
				c.validateWorkActions(t, fakeWorkClient.Actions())
			}

		})
	}
}
