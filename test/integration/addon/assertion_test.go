package integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

// TODO: The spec hash is hardcoded here and will break the test once the API changes.
// We need a better way to handle the spec hash.
// Config spec hashes (same for both v1alpha1 and v1beta1 since structure is identical)
const addOnDefaultConfigSpecHash = "ea4e6089e8b2f584c1cd8b86e16a15df3186b30744f433ede7436091c93a4cc7" //nolint:gosec
const addOnTest1ConfigSpecHash = "b746cc98936955dd2168640f0011a14f2a74be306c747bc8ba829328cc772690"   //nolint:gosec
const addOnTest2ConfigSpecHash = "3e6348b0c0f0a341ce28d0a2f99bc41fc63fb17d947eb5a45284729fbeb63b32"   //nolint:gosec
const addOnTest3ConfigSpecHash = "9b3c6d09731ed254589fa4fc2936e6fb00537de92da5e035183da313eb98d27e"   //nolint:gosec

// v1alpha1 config specs
var addOnDefaultConfigSpecAlpha = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test",
			Value: "test",
		},
	},
}

var addOnTest1ConfigSpecAlpha = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test1",
			Value: "test1",
		},
	},
}

var addOnTest2ConfigSpecAlpha = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test2",
			Value: "test2",
		},
	},
}

var addOnTest3ConfigSpecAlpha = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test3",
			Value: "test3",
		},
	},
}

// v1beta1 config specs (structure is identical to v1alpha1)
var addOnDefaultConfigSpecBeta = addonapiv1beta1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1beta1.CustomizedVariable{
		{
			Name:  "test",
			Value: "test",
		},
	},
}

var addOnTest1ConfigSpecBeta = addonapiv1beta1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1beta1.CustomizedVariable{
		{
			Name:  "test1",
			Value: "test1",
		},
	},
}

var addOnTest2ConfigSpecBeta = addonapiv1beta1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1beta1.CustomizedVariable{
		{
			Name:  "test2",
			Value: "test2",
		},
	},
}

var addOnTest3ConfigSpecBeta = addonapiv1beta1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1beta1.CustomizedVariable{
		{
			Name:  "test3",
			Value: "test3",
		},
	},
}

func createClusterManagementAddOnAlpha(ctx context.Context, name, defaultConfigNamespace,
	defaultConfigName string) (*addonapiv1alpha1.ClusterManagementAddOn, error) {
	clusterManagementAddon, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		clusterManagementAddon, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(
			ctx,
			&addonapiv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
					SupportedConfigs: []addonapiv1alpha1.ConfigMeta{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    addOnDeploymentConfigGVR.Group,
								Resource: addOnDeploymentConfigGVR.Resource,
							},
							DefaultConfig: &addonapiv1alpha1.ConfigReferent{
								Name:      defaultConfigName,
								Namespace: defaultConfigNamespace,
							},
						},
					},
					InstallStrategy: addonapiv1alpha1.InstallStrategy{
						Type: addonapiv1alpha1.AddonInstallStrategyManual,
					},
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			return nil, err
		}
		return clusterManagementAddon, nil
	}

	if err != nil {
		return nil, err
	}

	return clusterManagementAddon, nil
}

func createClusterManagementAddOnBeta(ctx context.Context, name, defaultConfigNamespace,
	defaultConfigName string) (*addonapiv1beta1.ClusterManagementAddOn, error) {
	clusterManagementAddon, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		clusterManagementAddon, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Create(
			ctx,
			&addonapiv1beta1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: addonapiv1beta1.ClusterManagementAddOnSpec{
					DefaultConfigs: []addonapiv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    addOnDeploymentConfigGVR.Group,
								Resource: addOnDeploymentConfigGVR.Resource,
							},
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Name:      defaultConfigName,
								Namespace: defaultConfigNamespace,
							},
						},
					},
					InstallStrategy: addonapiv1beta1.InstallStrategy{
						Type: addonapiv1beta1.AddonInstallStrategyManual,
					},
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			return nil, err
		}
		return clusterManagementAddon, nil
	}

	if err != nil {
		return nil, err
	}

	return clusterManagementAddon, nil
}

func patchClusterManagementAddOnAlpha(ctx context.Context, newaddon *addonapiv1alpha1.ClusterManagementAddOn) {
	Eventually(func() error {
		old, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, newaddon.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		new := old.DeepCopy()
		new.Spec = *newaddon.Spec.DeepCopy()
		new.Annotations = newaddon.Annotations

		cmaPatcher := patcher.NewPatcher[
			*addonapiv1alpha1.ClusterManagementAddOn, addonapiv1alpha1.ClusterManagementAddOnSpec, addonapiv1alpha1.ClusterManagementAddOnStatus](
			hubAddonClient.AddonV1alpha1().ClusterManagementAddOns())
		_, err = cmaPatcher.PatchSpec(ctx, new, new.Spec, old.Spec)
		if err != nil {
			return err
		}

		_, err = cmaPatcher.PatchLabelAnnotations(ctx, new, new.ObjectMeta, old.ObjectMeta)
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func patchClusterManagementAddOnBeta(ctx context.Context, newaddon *addonapiv1beta1.ClusterManagementAddOn) {
	Eventually(func() error {
		old, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, newaddon.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		new := old.DeepCopy()
		new.Spec = *newaddon.Spec.DeepCopy()
		new.Annotations = newaddon.Annotations

		_, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Update(ctx, new, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func updateManagedClusterAddOnStatusAlpha(ctx context.Context, new *addonapiv1alpha1.ManagedClusterAddOn) {
	Eventually(func() error {
		old, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Get(ctx, new.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		old.Status = new.Status
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(old.Namespace).UpdateStatus(ctx, old, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func updateManagedClusterAddOnStatusBeta(ctx context.Context, new *addonapiv1beta1.ManagedClusterAddOn) {
	Eventually(func() error {
		old, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(new.Namespace).Get(ctx, new.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		old.Status = new.Status
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(old.Namespace).UpdateStatus(ctx, old, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func assertClusterManagementAddOnDefaultConfigReferencesAlpha(ctx context.Context, name string, expect ...addonapiv1alpha1.DefaultConfigReference) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s DefaultConfigReferences (Alpha)", name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.DefaultConfigReferences) != len(expect) {
			return fmt.Errorf("expected %v default config reference, actual: %v", len(expect), len(actual.Status.DefaultConfigReferences))
		}

		for i, e := range expect {
			actualConfigReference := actual.Status.DefaultConfigReferences[i]

			if !apiequality.Semantic.DeepEqual(actualConfigReference, e) {
				return fmt.Errorf("expected default config is %v, actual: %v", e, actualConfigReference)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertClusterManagementAddOnDefaultConfigReferencesBeta(ctx context.Context, name string, expect ...addonapiv1beta1.DefaultConfigReference) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s DefaultConfigReferences (Beta)", name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.DefaultConfigReferences) != len(expect) {
			return fmt.Errorf("expected %v default config reference, actual: %v", len(expect), len(actual.Status.DefaultConfigReferences))
		}

		for i, e := range expect {
			actualConfigReference := actual.Status.DefaultConfigReferences[i]

			if !apiequality.Semantic.DeepEqual(actualConfigReference, e) {
				return fmt.Errorf("expected default config is %v, actual: %v", e, actualConfigReference)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func createAddOnDeploymentConfigAlpha(ctx context.Context,
	hubAddonClient addonclient.Interface,
	configNamespace string,
	configName string,
	configSpec addonapiv1alpha1.AddOnDeploymentConfigSpec,
) error {
	config := &addonapiv1alpha1.AddOnDeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: configNamespace,
		},
		Spec: configSpec,
	}
	_, err := hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configNamespace).Create(
		ctx, config, metav1.CreateOptions{})
	return err
}

func createAddOnDeploymentConfigBeta(ctx context.Context,
	hubAddonClient addonclient.Interface,
	configNamespace string,
	configName string,
	configSpec addonapiv1beta1.AddOnDeploymentConfigSpec,
) error {
	config := &addonapiv1beta1.AddOnDeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: configNamespace,
		},
		Spec: configSpec,
	}
	_, err := hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(configNamespace).Create(
		ctx, config, metav1.CreateOptions{})
	return err
}

func updateAddOnDeploymentConfigSpecAlpha(ctx context.Context,
	hubAddonClient addonclient.Interface,
	configNamespace string,
	configName string,
	newConfigSpec addonapiv1alpha1.AddOnDeploymentConfigSpec,
) {
	Eventually(func() error {
		addOnConfig, err := hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configNamespace).Get(
			ctx, configName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		addOnConfig.Spec = newConfigSpec
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configNamespace).Update(
			ctx, addOnConfig, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func updateAddOnDeploymentConfigSpecBeta(ctx context.Context,
	hubAddonClient addonclient.Interface,
	configNamespace string,
	configName string,
	newConfigSpec addonapiv1beta1.AddOnDeploymentConfigSpec,
) {
	Eventually(func() error {
		addOnConfig, err := hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(configNamespace).Get(
			ctx, configName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		addOnConfig.Spec = newConfigSpec
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(configNamespace).Update(
			ctx, addOnConfig, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func assertClusterManagementAddOnInstallProgressionAlpha(ctx context.Context, name string, expect ...addonapiv1alpha1.InstallProgression) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s InstallProgression", name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.InstallProgressions) != len(expect) {
			return fmt.Errorf("expected %v install progression, actual: %v", len(expect), len(actual.Status.InstallProgressions))
		}

		for i, e := range expect {
			actualInstallProgression := actual.Status.InstallProgressions[i]

			if !apiequality.Semantic.DeepEqual(actualInstallProgression.ConfigReferences, e.ConfigReferences) {
				return fmt.Errorf("expected InstallProgression.ConfigReferences is %v, actual: %v", e.ConfigReferences, actualInstallProgression.ConfigReferences)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertClusterManagementAddOnInstallProgressionBeta(ctx context.Context, name string, expect ...addonapiv1beta1.InstallProgression) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s InstallProgression (v1beta1)", name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.InstallProgressions) != len(expect) {
			return fmt.Errorf("expected %v install progression, actual: %v", len(expect), len(actual.Status.InstallProgressions))
		}

		for i, e := range expect {
			actualInstallProgression := actual.Status.InstallProgressions[i]

			if !apiequality.Semantic.DeepEqual(actualInstallProgression.ConfigReferences, e.ConfigReferences) {
				return fmt.Errorf("expected InstallProgression.ConfigReferences is %v, actual: %v", e.ConfigReferences, actualInstallProgression.ConfigReferences)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertManagedClusterAddOnConfigReferencesAlpha(ctx context.Context, name, namespace string, expect ...addonapiv1alpha1.ConfigReference) {
	By(fmt.Sprintf("Check ManagedClusterAddOn %s/%s ConfigReferences", namespace, name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.ConfigReferences) != len(expect) {
			return fmt.Errorf("expected %v config reference, actual: %v", len(expect), len(actual.Status.ConfigReferences))
		}

		for i, e := range expect {
			actualConfigReference := actual.Status.ConfigReferences[i]

			if !apiequality.Semantic.DeepEqual(actualConfigReference, e) {
				return fmt.Errorf("expected mca config reference is %v %v, actual: %v %v",
					e.DesiredConfig,
					e.LastAppliedConfig,
					actualConfigReference.DesiredConfig,
					actualConfigReference.LastAppliedConfig,
				)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertManagedClusterAddOnConfigReferencesBeta(ctx context.Context, name, namespace string, expect ...addonapiv1beta1.ConfigReference) {
	By(fmt.Sprintf("Check ManagedClusterAddOn %s/%s ConfigReferences (v1beta1)", namespace, name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.ConfigReferences) != len(expect) {
			return fmt.Errorf("expected %v config reference, actual: %v", len(expect), len(actual.Status.ConfigReferences))
		}

		for i, e := range expect {
			actualConfigReference := actual.Status.ConfigReferences[i]

			if !apiequality.Semantic.DeepEqual(actualConfigReference, e) {
				return fmt.Errorf("expected mca config reference is %v %v, actual: %v %v",
					e.DesiredConfig,
					e.LastAppliedConfig,
					actualConfigReference.DesiredConfig,
					actualConfigReference.LastAppliedConfig,
				)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertClusterManagementAddOnAnnotationsAlpha(ctx context.Context, name string) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %v Annotations (Alpha)", name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if _, ok := actual.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey]; ok {
			return fmt.Errorf("expected annotation %v to be empty, actual: %v",
				addonapiv1alpha1.AddonLifecycleAnnotationKey,
				actual.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey])
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertClusterManagementAddOnAnnotationsBeta(ctx context.Context, name string) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %v Annotations (Beta)", name))

	// Note: The AddonLifecycleAnnotationKey was removed in v1beta1, so this assertion
	// is a no-op for backward compatibility with test structure. In v1beta1, addon
	// lifecycle management is always handled by the addon-manager.
	Eventually(func() error {
		_, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func updateManifestWorkStatus(ctx context.Context, client workclientset.Interface, clusterName string, manifestWorkName string, status metav1.ConditionStatus) {
	Eventually(func() error {
		work, err := client.WorkV1().ManifestWorks(clusterName).Get(ctx, manifestWorkName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		meta.SetStatusCondition(
			&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkApplied, Status: status, Reason: "WorkApplied", ObservedGeneration: work.Generation})
		meta.SetStatusCondition(
			&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkAvailable, Status: status, Reason: "WorkAvailable", ObservedGeneration: work.Generation})

		_, err = client.WorkV1().ManifestWorks(clusterName).UpdateStatus(ctx, work, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func createManagedCluster(ctx context.Context, hubClusterClient clusterv1client.Interface, managedClusterName string) error {
	managedCluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedClusterName,
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: true,
		},
	}
	_, err := hubClusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
	_, err = hubKubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func createPlacementDecision(ctx context.Context, hubClusterClient clusterv1client.Interface,
	placementNamespace, placementName, groupIndex string, clusterNames ...string) error {
	var err error
	decision := &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      placementName + groupIndex,
			Namespace: placementNamespace,
			Labels: map[string]string{
				clusterv1beta1.PlacementLabel:          placementName,
				clusterv1beta1.DecisionGroupIndexLabel: groupIndex,
			},
		},
	}
	decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Create(ctx, decision, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	clusterDecisions := []clusterv1beta1.ClusterDecision{}
	for _, clusterName := range clusterNames {
		clusterDecisions = append(clusterDecisions, clusterv1beta1.ClusterDecision{ClusterName: clusterName})
	}
	decision.Status.Decisions = clusterDecisions

	_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(ctx, decision, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func assertClusterManagementAddOnConditionsAlpha(ctx context.Context, name string, expect ...metav1.Condition) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s Conditions (Alpha)", name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for i, ec := range expect {
			if i >= len(actual.Status.InstallProgressions) {
				return fmt.Errorf("expected %d install progressions, actual: %d", i+1, len(actual.Status.InstallProgressions))
			}
			cond := meta.FindStatusCondition(actual.Status.InstallProgressions[i].Conditions, ec.Type)
			if cond == nil ||
				cond.Status != ec.Status ||
				cond.Reason != ec.Reason ||
				cond.Message != ec.Message {
				return fmt.Errorf("expected cma progressing condition is %v, actual: %v", ec, cond)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertClusterManagementAddOnConditionsBeta(ctx context.Context, name string, expect ...metav1.Condition) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s Conditions (Beta)", name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for i, ec := range expect {
			if i >= len(actual.Status.InstallProgressions) {
				return fmt.Errorf("expected %d install progressions, actual: %d", i+1, len(actual.Status.InstallProgressions))
			}
			cond := meta.FindStatusCondition(actual.Status.InstallProgressions[i].Conditions, ec.Type)
			if cond == nil ||
				cond.Status != ec.Status ||
				cond.Reason != ec.Reason ||
				cond.Message != ec.Message {
				return fmt.Errorf("expected cma progressing condition is %v, actual: %v", ec, cond)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

// earliestRolloutDeadline returns the earliest time at which the rollout may report a timeout
// for a progression that started at start.
//
// The rollout counts the deadline from the LastTransitionTime of the addon Progressing
// condition, which is serialized with second granularity. The stored timestamp is therefore up
// to one second earlier than the moment the controller recorded it, and the timeout can fire
// that much before start+duration without the controller being wrong. Truncating start to the
// second gives the same lower bound the controller works with, so the assertions below can
// check the whole interval leading up to it instead of leaving a blind spot at the end.
func earliestRolloutDeadline(start metav1.Time, duration time.Duration) time.Time {
	return start.Time.Truncate(time.Second).Add(duration)
}

func assertClusterManagementAddOnNoConditionsAlpha(ctx context.Context, name string, start metav1.Time, duration time.Duration, expect ...metav1.Condition) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s no conditions in duration %v (Alpha)", name, duration))

	notBefore := earliestRolloutDeadline(start, duration)

	Consistently(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Once the deadline is reached the condition is allowed to show up at any time
		now := metav1.Now()
		if !now.Time.Before(notBefore) {
			return nil
		}

		for i, ec := range expect {
			if i >= len(actual.Status.InstallProgressions) {
				return fmt.Errorf("expected %d install progressions, actual: %d", i+1, len(actual.Status.InstallProgressions))
			}
			cond := meta.FindStatusCondition(actual.Status.InstallProgressions[i].Conditions, ec.Type)

			// The expected timeout condition should NOT appear before the duration
			if cond != nil &&
				cond.Status == ec.Status &&
				cond.Reason == ec.Reason &&
				cond.Message == ec.Message {
				return fmt.Errorf("unexpected condition matches %v before the %v deadline %v",
					now.Time, duration, notBefore)
			}
		}

		return nil
	}, duration+2*time.Second, eventuallyInterval).Should(BeNil())
}

func assertClusterManagementAddOnNoConditionsBeta(ctx context.Context, name string, start metav1.Time, duration time.Duration, expect ...metav1.Condition) {
	By(fmt.Sprintf("Check ClusterManagementAddOn %s no conditions in duration %v (Beta)", name, duration))

	notBefore := earliestRolloutDeadline(start, duration)

	Consistently(func() error {
		actual, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Once the deadline is reached the condition is allowed to show up at any time
		now := metav1.Now()
		if !now.Time.Before(notBefore) {
			return nil
		}

		for i, ec := range expect {
			if i >= len(actual.Status.InstallProgressions) {
				return fmt.Errorf("expected %d install progressions, actual: %d", i+1, len(actual.Status.InstallProgressions))
			}
			cond := meta.FindStatusCondition(actual.Status.InstallProgressions[i].Conditions, ec.Type)

			// The expected timeout condition should NOT appear before the duration
			if cond != nil &&
				cond.Status == ec.Status &&
				cond.Reason == ec.Reason &&
				cond.Message == ec.Message {
				return fmt.Errorf("unexpected condition matches %v before the %v deadline %v",
					now.Time, duration, notBefore)
			}
		}

		return nil
	}, duration+2*time.Second, eventuallyInterval).Should(BeNil())
}

func assertManagedClusterAddOnConditionsAlpha(ctx context.Context, name, namespace string, expect ...metav1.Condition) {
	By(fmt.Sprintf("Check ManagedClusterAddOn %s/%s Conditions (Alpha)", namespace, name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, ec := range expect {
			cond := meta.FindStatusCondition(actual.Status.Conditions, ec.Type)
			if cond == nil ||
				cond.Status != ec.Status ||
				cond.Reason != ec.Reason ||
				cond.Message != ec.Message {
				return fmt.Errorf("expected addon progressing condition is %v, actual: %v", ec, cond)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertManagedClusterAddOnConditionsBeta(ctx context.Context, name, namespace string, expect ...metav1.Condition) {
	By(fmt.Sprintf("Check ManagedClusterAddOn %s/%s Conditions (Beta)", namespace, name))

	Eventually(func() error {
		actual, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, ec := range expect {
			cond := meta.FindStatusCondition(actual.Status.Conditions, ec.Type)
			if cond == nil ||
				cond.Status != ec.Status ||
				cond.Reason != ec.Reason ||
				cond.Message != ec.Message {
				return fmt.Errorf("expected addon progressing condition is %v, actual: %v", ec, cond)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}
