package addonprogressing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		work                   []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKey:                "test/test",
			clusterManagementAddon: []runtime.Object{},
			managedClusteraddon:    []runtime.Object{},
			work:                   []runtime.Object{},
			validateAddonActions:   testingcommon.AssertNoActions,
		},
		{
			name:                   "no managedClusteraddon",
			syncKey:                "test/test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions:   testingcommon.AssertNoActions,
		},
		{
			name:    "no work applied condition",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == "WaitingForManifestApplied" &&
					configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when no work",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when work config spec not match",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "hash1new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "hash2new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hash\"}",
				})
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
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when work is not ready",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionFalse,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to uprading when work config spec not match",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "hash1new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "hash2new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hash\"}",
				})
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
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to uprading when work is not ready",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionFalse,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to install succeed",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "hash1new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "hash2new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test1\":\"hash1new\"," +
						"\"foo.core/open-cluster-management/test2\":\"hash2new\"}",
				})
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
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonCompleted && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 2 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
				if addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash != addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("LastAppliedConfig object is not correct: %v", addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash)
				}
			},
		},
		{
			name:    "update managedclusteraddon to upgrade succeed",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "hash1new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "hash2new",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test1\":\"hash1new\"," +
						"\"foo.core/open-cluster-management/test2\":\"hash2new\"}",
				})
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
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonCompleted && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 2 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
				if addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash != addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("LastAppliedConfig object is not correct: %v", addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash)
				}
			},
		},
		{
			name:    "works for hosted and default addon in the same namespace",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
					Message: "manifests of addon are applied successfully",
				})
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work: func() []runtime.Object {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey: "test",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
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
				hostedWork := addontesting.NewManifestWork(
					"addon-test-deploy-hosting-another-cluster",
					"cluster1",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
				)
				hostedWork.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "another-cluster",
				})
				return []runtime.Object{work, hostedWork}
			}(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonCompleted && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 1 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
				if addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash != addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("LastAppliedConfig object is not correct: %v", addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash)
				}
			},
		},
		{
			name:    "update managedclusteraddon to configuration unsupported...",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "config1.test",
								Resource: "config1",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "override",
							},
						},
					}
					addon.Status.SupportedConfigs = []addonapiv1alpha1.ConfigGroupResource{
						{
							Group:    "configs.test",
							Resource: "testconfigs",
						},
					}
					meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					})
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			work:                   []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}

				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonConfigurationUnsupported && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)
			fakeWorkClient := fakework.NewSimpleClientset()

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.work {
				if err := workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)

			controller := NewAddonProgressingController(
				fakeAddonClient,
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
				addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
				workInformers.Work().V1().ManifestWorks(),
				utils.ManagedByAddonManager,
			)

			err := controller.Sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}

func TestReconcileHostedAddons(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		work                   []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:    "no work applied condition",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster"),
			},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == "WaitingForManifestApplied" &&
					configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
		{
			name:    "no hosting work applied condition",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
				),
			},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == "WaitingForHostingManifestApplied" &&
					configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when no work",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "hosting manifests of addon are applied successfully",
					},
				),
			},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing &&
					configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when work config spec not match",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "hosting manifests of addon are applied successfully",
					},
				)
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{
				func() *workapiv1.ManifestWork {
					work := addontesting.NewManifestWork(
						"addon-test-deploy",
						"hosting-cluster",
						testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
						testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
					)
					work.SetLabels(map[string]string{
						addonapiv1alpha1.AddonLabelKey:          "test",
						addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
					})
					work.SetAnnotations(map[string]string{
						workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hash\"}",
					})
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
					return work
				}(),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing &&
					configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to installing when work is not ready",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "hosting manifests of addon are applied successfully",
					},
				)
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"hosting-cluster",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionFalse,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionTrue,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing && configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to uprading when work config spec not match",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "hosting manifests of addon are applied successfully",
					},
				)
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"hosting-cluster",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hash\"}",
				})
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
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing &&
					configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to uprading when work is not ready",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "hosting manifests of addon are applied successfully",
					},
				)
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"hosting-cluster",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
				work.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workapiv1.WorkAvailable,
						Status: metav1.ConditionFalse,
					},
				}
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == addonapiv1alpha1.ProgressingReasonProgressing &&
					configCond.Status == metav1.ConditionTrue) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 0 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
			},
		},
		{
			name:    "update managedclusteraddon to install succeed",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "hosting manifests of addon are applied successfully",
					},
				)
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"hosting-cluster",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
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
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == addonapiv1alpha1.ProgressingReasonCompleted &&
					configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 1 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
				if addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash !=
					addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("LastAppliedConfig object is not correct: %v",
						addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash)
				}
			},
		},
		{
			name:    "update managedclusteraddon to upgrade succeed",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "manifests of addon are applied successfully",
					},
					metav1.Condition{
						Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
						Status:  metav1.ConditionTrue,
						Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
						Message: "hosting manifests of addon are applied successfully",
					},
				)
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hashnew",
						},
						LastAppliedConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test", Namespace: "open-cluster-management"},
							SpecHash:       "hash",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{func() *workapiv1.ManifestWork {
				work := addontesting.NewManifestWork(
					"addon-test-deploy",
					"hosting-cluster",
					testingcommon.NewUnstructured("v1", "ConfigMap", "default", "test1"),
					testingcommon.NewUnstructured("v1", "Deployment", "default", "test1"),
				)
				work.SetLabels(map[string]string{
					addonapiv1alpha1.AddonLabelKey:          "test",
					addonapiv1alpha1.AddonNamespaceLabelKey: "cluster1",
				})
				work.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "{\"foo.core/open-cluster-management/test\":\"hashnew\"}",
				})
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
				return work
			}()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch

				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(actual, addOn)
				if err != nil {
					t.Fatal(err)
				}
				configCond := meta.FindStatusCondition(
					addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil &&
					configCond.Reason == addonapiv1alpha1.ProgressingReasonCompleted &&
					configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
				if len(addOn.Status.ConfigReferences) != 1 {
					t.Errorf("ConfigReferences object is not correct: %v", addOn.Status.ConfigReferences)
				}
				if addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash !=
					addOn.Status.ConfigReferences[0].DesiredConfig.SpecHash {
					t.Errorf("LastAppliedConfig object is not correct: %v",
						addOn.Status.ConfigReferences[0].LastAppliedConfig.SpecHash)
				}
			},
		},
		{
			name:    "update managedclusteraddon to configuration unsupported...",
			syncKey: "cluster1/test",
			managedClusteraddon: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewHostedModeAddon("test", "cluster1", "hosting-cluster",
						metav1.Condition{
							Type:    addonapiv1alpha1.ManagedClusterAddOnManifestApplied,
							Status:  metav1.ConditionTrue,
							Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
							Message: "manifests of addon are applied successfully",
						},
						metav1.Condition{
							Type:    addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied,
							Status:  metav1.ConditionTrue,
							Reason:  addonapiv1alpha1.AddonManifestAppliedReasonManifestsApplied,
							Message: "hosting manifests of addon are applied successfully",
						},
					)
					addon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "config1.test",
								Resource: "config1",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "override",
							},
						},
					}
					addon.Status.SupportedConfigs = []addonapiv1alpha1.ConfigGroupResource{
						{
							Group:    "configs.test",
							Resource: "testconfigs",
						},
					}
					return addon
				}(),
			},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build(),
			},
			work: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				addOn := &addonapiv1alpha1.ManagedClusterAddOn{}
				err := json.Unmarshal(patch, addOn)
				if err != nil {
					t.Fatal(err)
				}

				configCond := meta.FindStatusCondition(addOn.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnConditionProgressing)
				if !(configCond != nil && configCond.Reason == addonapiv1alpha1.ProgressingReasonConfigurationUnsupported && configCond.Status == metav1.ConditionFalse) {
					t.Errorf("Condition Progressing is incorrect")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)
			fakeWorkClient := fakework.NewSimpleClientset()

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().
					Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().
					Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.work {
				if err := workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)

			controller := NewAddonProgressingController(
				fakeAddonClient,
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
				addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
				workInformers.Work().V1().ManifestWorks(),
				utils.ManagedByAddonManager,
			)

			err := controller.Sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
