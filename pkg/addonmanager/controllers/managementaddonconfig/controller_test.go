package managementaddonconfig

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
		name                   string
		syncKey                string
		clusterManagementAddon []runtime.Object
		configs                []runtime.Object
		validateActions        func(*testing.T, []clienttesting.Action)
	}{
		{
			name:                   "no cma",
			syncKey:                "test",
			clusterManagementAddon: []runtime.Object{},
			configs:                []runtime.Object{newTestConfing("test", "cluster1", 1)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:                   "no config reference in status",
			syncKey:                "test",
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").Build()},
			configs:                []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:    "no configs",
			syncKey: "test",
			clusterManagementAddon: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("test", "", "").Build()
					cma.Status.DefaultConfigReferences = []addonapiv1alpha1.DefaultConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "other",
								Resource: "other",
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
								SpecHash: "",
							},
						},
					}
					return cma
				}(),
			},
			configs: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
		},
		{
			name:    "update default config spec hash",
			syncKey: "test",
			clusterManagementAddon: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("test", "", "").Build()
					cma.Status.DefaultConfigReferences = []addonapiv1alpha1.DefaultConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
								SpecHash: "",
							},
						},
					}
					return cma
				}(),
			},
			configs: []runtime.Object{newTestConfing("test", "cluster1", 2)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if cma.Status.DefaultConfigReferences[0].DesiredConfig.SpecHash != "3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04" {
					t.Errorf("Expect addon config spec hash is 3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04, but got %v", cma.Status.DefaultConfigReferences[0].DesiredConfig.SpecHash)
				}
			},
		},
		{
			name:    "update install progression config spec hash",
			syncKey: "test",
			clusterManagementAddon: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("test", "", "").Build()
					cma.Status.InstallProgressions = []addonapiv1alpha1.InstallProgression{{
						PlacementRef: addonapiv1alpha1.PlacementRef{
							Name:      "test",
							Namespace: "test",
						},
						ConfigReferences: []addonapiv1alpha1.InstallConfigReference{{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
								SpecHash: "",
							},
						}},
					}}
					return cma
				}(),
			},
			configs: []runtime.Object{newTestConfing("test", "cluster1", 2)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.SpecHash != "3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04" {
					t.Errorf("Expect addon config spec hash is 3e80b3778b3b03766e7be993131c0af2ad05630c5d96fb7fa132d05b77336e04, but got %v", cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.SpecHash)
				}
			},
		},
		{
			name:    "config has same spec hash when spec are same",
			syncKey: "test",
			clusterManagementAddon: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("test", "", "").Build()
					cma.Status.InstallProgressions = []addonapiv1alpha1.InstallProgression{
						{
							PlacementRef: addonapiv1alpha1.PlacementRef{
								Name:      "test",
								Namespace: "test",
							},
							ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
								{
									ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
										Group:    fakeGVR.Group,
										Resource: fakeGVR.Resource,
									},
									DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
										ConfigReferent: addonapiv1alpha1.ConfigReferent{
											Namespace: "cluster1",
											Name:      "test1",
										},
										SpecHash: "",
									},
								},
							},
						},
						{
							PlacementRef: addonapiv1alpha1.PlacementRef{
								Name:      "test",
								Namespace: "test",
							},
							ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
								{
									ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
										Group:    fakeGVR.Group,
										Resource: fakeGVR.Resource,
									},
									DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
										ConfigReferent: addonapiv1alpha1.ConfigReferent{
											Namespace: "cluster1",
											Name:      "test2",
										},
										SpecHash: "",
									},
								},
							},
						},
					}
					return cma
				}(),
			},
			configs: []runtime.Object{newTestConfing("test1", "cluster1", 2), newTestConfing("test2", "cluster1", 2)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				patch := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(patch, cma)
				if err != nil {
					t.Fatal(err)
				}

				if cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.SpecHash != cma.Status.InstallProgressions[1].ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("Expect addon config spec hash to be same, but got %v and %v", cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.SpecHash, cma.Status.InstallProgressions[1].ConfigReferences[0].DesiredConfig.SpecHash)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.clusterManagementAddon...)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			addonStore := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore()
			for _, addon := range c.clusterManagementAddon {
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

			ctrl := &clusterManagementAddonConfigController{
				addonClient:                  fakeAddonClient,
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				configListers:                map[schema.GroupResource]dynamiclister.Lister{},
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
		cmas              []runtime.Object
		config            runtime.Object
		expectedQueueSize int
	}{
		{
			name:              "no config reference",
			cmas:              []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").Build()},
			config:            newTestConfing("test", "cluster1", 1),
			expectedQueueSize: 0,
		},
		{
			name: "has config reference",
			cmas: []runtime.Object{
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("test1", "", "").Build()
					cma.Status.DefaultConfigReferences = []addonapiv1alpha1.DefaultConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
								SpecHash: "",
							},
						},
					}
					return cma
				}(),
				func() *addonapiv1alpha1.ClusterManagementAddOn {
					cma := addontesting.NewClusterManagementAddon("test2", "", "").Build()
					cma.Status.InstallProgressions = []addonapiv1alpha1.InstallProgression{{
						PlacementRef: addonapiv1alpha1.PlacementRef{
							Name:      "test",
							Namespace: "test",
						},
						ConfigReferences: []addonapiv1alpha1.InstallConfigReference{{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    fakeGVR.Group,
								Resource: fakeGVR.Resource,
							},
							DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
								ConfigReferent: addonapiv1alpha1.ConfigReferent{
									Namespace: "cluster1",
									Name:      "test",
								},
								SpecHash: "",
							},
						}},
					}}
					return cma
				}(),
			},
			config:            newTestConfing("test", "cluster1", 1),
			expectedQueueSize: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.cmas...)
			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			addonInformer := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer()

			ctrl := &clusterManagementAddonConfigController{
				clusterManagementAddonIndexer: addonInformer.GetIndexer(),
				queue:                         addontesting.NewFakeSyncContext(t).Queue(),
			}

			if err := addonInformer.AddIndexers(cache.Indexers{byClusterManagementAddOnConfig: ctrl.indexByConfig}); err != nil {
				t.Fatal(err)
			}
			addonStore := addonInformer.GetStore()
			for _, addon := range c.cmas {
				if err := addonStore.Add(addon); err != nil {
					t.Fatal(err)
				}
			}

			ctrl.enqueueClusterManagementAddOnsByConfig(fakeGVR)(c.config)

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
