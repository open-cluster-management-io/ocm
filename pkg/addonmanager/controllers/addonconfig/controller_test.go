package addonconfig

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
)

var fakeGVR = schema.GroupVersionResource{
	Group:    "configs.test",
	Version:  "v1",
	Resource: "configs",
}

func TestSync(t *testing.T) {
	cases := []struct {
		name                string
		syncKey             string
		managedClusteraddon []runtime.Object
		configs             []runtime.Object
		validateActions     func(*testing.T, []clienttesting.Action)
	}{
		{
			name:                "no configs",
			syncKey:             "test/test",
			managedClusteraddon: []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			configs:             []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:                "no addons",
			syncKey:             "cluster1/test",
			managedClusteraddon: []runtime.Object{},
			configs:             []runtime.Object{newTestConfing("test", "cluster1", 1)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:    "supported Configs",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
						},
					}
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
							LastObservedGeneration: 1,
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
							},
						},
					}
					addon.Status.SupportedConfigs = []addonapiv1alpha1.ConfigGroupResource{
						{
							Group:    fakeGVR.Group,
							Resource: fakeGVR.Resource,
						},
					}
					return addon
				}(),
			},
			configs: []runtime.Object{newTestConfing("test", "cluster1", 2)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}

				if addOn.Status.ConfigReferences[0].LastObservedGeneration != 2 {
					t.Errorf("Expect addon config generation is 2, but got %v", addOn.Status.ConfigReferences[0].LastObservedGeneration)
				}

				if addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash != "3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04" {
					t.Errorf("Expect addon config spec hash is 3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04, but got %v", addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash)
				}
			},
		},
		{
			name:    "unsupported configs",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
						},
					}
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
							LastObservedGeneration: 1,
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
							},
						},
					}
					return addon
				}(),
			},
			configs: []runtime.Object{newTestConfing("test", "cluster1", 2)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}

				if addOn.Status.ConfigReferences[0].LastObservedGeneration != 2 {
					t.Errorf("Expect addon config generation is 2, but got %v", addOn.Status.ConfigReferences[0].LastObservedGeneration)
				}

				if addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash != "" {
					t.Errorf("Expect addon config spec hash is empty, but got %v", addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash)
				}
			},
		},
		{
			name:    "update generation for all the configs in status",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
							LastObservedGeneration: 1,
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
							},
						},
					}
					return addon
				}(),
			},
			configs: []runtime.Object{newTestConfing("test", "cluster1", 2)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}

				if addOn.Status.ConfigReferences[0].LastObservedGeneration != 2 {
					t.Errorf("Expect addon config generation is 2, but got %v", addOn.Status.ConfigReferences[0].LastObservedGeneration)
				}

				if addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash != "" {
					t.Errorf("Expect addon config spec hash is empty, but got %v", addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash)
				}
			},
		},
		{
			name:    "no status",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
						},
					}
					return addon
				}(),
			},
			configs: []runtime.Object{newTestConfing("test", "cluster1", 1)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:    "no configs",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
						},
					}
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
							LastObservedGeneration: 1,
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
							},
						},
					}

					return addon
				}(),
			},
			configs:         []runtime.Object{},
			validateActions: addontesting.AssertNoActions,
		},

		{
			name:    "cluster scope config",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test",
							},
						},
					}
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test",
							},
							LastObservedGeneration: 2,
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Name: "test",
								},
							},
						},
					}
					addon.Status.SupportedConfigs = []addonapiv1alpha1.ConfigGroupResource{
						{
							Group:    fakeGVR.Group,
							Resource: fakeGVR.Resource,
						},
					}
					return addon
				}(),
			},
			configs: []runtime.Object{newTestConfing("test", "", 3)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}
				if addOn.Status.ConfigReferences[0].LastObservedGeneration != 3 {
					t.Errorf("Expect addon config generation is 3, but got %v", addOn.Status.ConfigReferences[0].LastObservedGeneration)
				}
				if addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash != "3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04" {
					t.Errorf("Expect addon config spec hash is 3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04, but got %v", addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			addonStore := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
			for _, addon := range c.managedClusteraddon {
				if err := addonStore.Add(addon); err != nil {
					t.Fatal(err)
				}
			}

			fakeDynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			configInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(fakeDynamicClient, 0)
			configInformer := configInformerFactory.ForResource(fakeGVR)
			configStore := configInformer.Informer().GetStore()
			for _, config := range c.configs {
				if err := configStore.Add(config); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := addontesting.NewFakeSyncContext(t)

			ctrl := &addonConfigController{
				addonClient:   fakeAddonClient,
				addonLister:   addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				configListers: map[schema.GroupResource]dynamiclister.Lister{},
			}

			ctrl.buildConfigInformers(configInformerFactory, map[schema.GroupVersionResource]bool{fakeGVR: true})

			err := ctrl.sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}

			c.validateActions(t, fakeAddonClient.Actions())
		})
	}
}

func TestEnqueue(t *testing.T) {
	cases := []struct {
		name              string
		addons            []runtime.Object
		config            runtime.Object
		expectedQueueSize int
	}{
		{
			name:              "no config reference",
			addons:            []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			config:            newTestConfing("test", "cluster1", 1),
			expectedQueueSize: 0,
		},
		{
			name: "configs in spec",
			addons: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "other",
								Resource: "other",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "other",
							},
						},
					}
					return addon
				}(),
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster2")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test",
							},
						},
					}
					return addon
				}(),
			},
			config:            newTestConfing("test", "", 1),
			expectedQueueSize: 0,
		},
		{
			name: "configs in status",
			addons: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "other",
								Resource: "other",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "other",
							},
						},
					}
					return addon
				}(),
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster2")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "test",
							},
						},
					}
					return addon
				}(),
			},
			config:            newTestConfing("test", "", 1),
			expectedQueueSize: 2,
		},
		{
			name: "owned configs",
			addons: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test1", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "test",
							},
							LastObservedGeneration: 1,
						},
					}
					return addon
				}(),
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test2", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "otherconfigs.test",
								Resource: "otherconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name: "other",
							},
							LastObservedGeneration: 1,
						},
					}
					return addon
				}(),
			},
			config:            newTestConfing("test", "cluster1", 1),
			expectedQueueSize: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addons...)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			addonInformer := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer()

			ctrl := &addonConfigController{
				addonIndexer: addonInformer.GetIndexer(),
				queue:        addontesting.NewFakeSyncContext(t).Queue(),
			}

			if err := addonInformer.AddIndexers(cache.Indexers{byAddOnConfig: ctrl.indexByConfig}); err != nil {
				t.Fatal(err)
			}
			addonStore := addonInformer.GetStore()
			for _, addon := range c.addons {
				if err := addonStore.Add(addon); err != nil {
					t.Fatal(err)
				}
			}

			ctrl.enqueueAddOnsByConfig(fakeGVR)(c.config)

			if c.expectedQueueSize != ctrl.queue.Len() {
				t.Errorf("expect queue %d item, but got %d", c.expectedQueueSize, ctrl.queue.Len())
			}
		})
	}
}

func newTestConfing(name, namespace string, generation int64) *unstructured.Unstructured {
	if generation == 0 {
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "config.test/v1",
				"kind":       "Config",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"test": "test",
				},
			},
		}
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.test/v1",
			"kind":       "Config",
			"metadata": map[string]interface{}{
				"name":       name,
				"namespace":  namespace,
				"generation": generation,
			},
			"spec": map[string]interface{}{
				"test": "test",
			},
		},
	}
}
