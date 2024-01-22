package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

// TODO: The spec hash is hardcoded here and will break the test once the API changes.
// We need a better way to handle the spec hash.
const addOnDefaultConfigSpecHash = "19afba0192e93f738a3b6e118f177dffbc6546f7d32a3aed79cba761e6123a1f" //nolint:gosec
const addOnTest1ConfigSpecHash = "1eef585d8f4d92a0d1aa5ac3a61337dd03c2723f9f8d1c56f339e0af0becd4af"   //nolint:gosec
const addOnTest2ConfigSpecHash = "c0f1c4105357e7a298a8789d88c7acffc83704f407a35d73fa0b83a079958440"   //nolint:gosec
const addOnTest3ConfigSpecHash = "83ca9715ea80ec4b88438607e17f15aaf1c2db8e469dfa933ceb5887cc49328d"   //nolint:gosec

var addOnDefaultConfigSpec = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test",
			Value: "test",
		},
	},
}
var addOnTest1ConfigSpec = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test1",
			Value: "test1",
		},
	},
}
var addOnTest2ConfigSpec = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test2",
			Value: "test2",
		},
	},
}

var addOnTest3ConfigSpec = addonapiv1alpha1.AddOnDeploymentConfigSpec{
	CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
		{
			Name:  "test3",
			Value: "test3",
		},
	},
}

func createClusterManagementAddOn(name, defaultConfigNamespace, defaultConfigName string) (*addonapiv1alpha1.ClusterManagementAddOn, error) {
	clusterManagementAddon, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		clusterManagementAddon, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(
			context.Background(),
			&addonapiv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Annotations: map[string]string{
						addonapiv1alpha1.AddonLifecycleAnnotationKey: addonapiv1alpha1.AddonLifecycleAddonManagerAnnotationValue,
					},
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

func patchClusterManagementAddOn(_ context.Context, newaddon *addonapiv1alpha1.ClusterManagementAddOn) {
	gomega.Eventually(func() error {
		old, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), newaddon.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		new := old.DeepCopy()
		new.Spec = *newaddon.Spec.DeepCopy()
		new.Annotations = newaddon.Annotations

		cmaPatcher := patcher.NewPatcher[
			*addonapiv1alpha1.ClusterManagementAddOn, addonapiv1alpha1.ClusterManagementAddOnSpec, addonapiv1alpha1.ClusterManagementAddOnStatus](
			hubAddonClient.AddonV1alpha1().ClusterManagementAddOns())
		_, err = cmaPatcher.PatchSpec(context.Background(), new, new.Spec, old.Spec)
		if err != nil {
			return err
		}

		_, err = cmaPatcher.PatchLabelAnnotations(context.Background(), new, new.ObjectMeta, old.ObjectMeta)
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
}

func updateManagedClusterAddOnStatus(_ context.Context, new *addonapiv1alpha1.ManagedClusterAddOn) {
	gomega.Eventually(func() error {
		old, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Get(context.Background(), new.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		old.Status = new.Status
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(old.Namespace).UpdateStatus(context.Background(), old, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
}

func assertClusterManagementAddOnDefaultConfigReferences(name string, expect ...addonapiv1alpha1.DefaultConfigReference) {
	ginkgo.By(fmt.Sprintf("Check ClusterManagementAddOn %s DefaultConfigReferences", name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
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
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func updateManifestWorkStatus(client workclientset.Interface, clusterName string, manifestWorkName string, status metav1.ConditionStatus) {
	gomega.Eventually(func() error {
		work, err := client.WorkV1().ManifestWorks(clusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		meta.SetStatusCondition(
			&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkApplied, Status: status, Reason: "WorkApplied", ObservedGeneration: work.Generation})
		meta.SetStatusCondition(
			&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkAvailable, Status: status, Reason: "WorkAvailable", ObservedGeneration: work.Generation})

		_, err = client.WorkV1().ManifestWorks(clusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
}

func createAddOnDeploymentConfig(
	hubAddonClient addonv1alpha1client.Interface,
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
		context.Background(), config, metav1.CreateOptions{})
	return err
}

func updateAddOnDeploymentConfigSpec(
	hubAddonClient addonv1alpha1client.Interface,
	configNamespace string,
	configName string,
	newConfigSpec addonapiv1alpha1.AddOnDeploymentConfigSpec,
) {
	gomega.Eventually(func() error {
		addOnConfig, err := hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configNamespace).Get(
			context.Background(), configName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		addOnConfig.Spec = newConfigSpec
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configNamespace).Update(
			context.Background(), addOnConfig, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
}

func createManagedCluster(hubClusterClient clusterv1client.Interface, managedClusterName string) error {
	managedCluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedClusterName,
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: true,
		},
	}
	_, err := hubClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
	_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func createPlacementDecision(hubClusterClient clusterv1client.Interface, placementNamespace, placementName, groupIndex string, clusterNames ...string) error {
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
	decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Create(context.Background(), decision, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	clusterDecisions := []clusterv1beta1.ClusterDecision{}
	for _, clusterName := range clusterNames {
		clusterDecisions = append(clusterDecisions, clusterv1beta1.ClusterDecision{ClusterName: clusterName})
	}
	decision.Status.Decisions = clusterDecisions

	_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func assertClusterManagementAddOnInstallProgression(name string, expect ...addonapiv1alpha1.InstallProgression) {
	ginkgo.By(fmt.Sprintf("Check ClusterManagementAddOn %s InstallProgression", name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
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
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertClusterManagementAddOnConditions(name string, expect ...metav1.Condition) {
	ginkgo.By(fmt.Sprintf("Check ClusterManagementAddOn %s Conditions", name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for i, ec := range expect {
			cond := meta.FindStatusCondition(actual.Status.InstallProgressions[i].Conditions, ec.Type)
			if cond == nil ||
				cond.Status != ec.Status ||
				cond.Reason != ec.Reason ||
				cond.Message != ec.Message {
				return fmt.Errorf("expected cma progressing condition is %v, actual: %v", ec, cond)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertClusterManagementAddOnNoConditions(name string, start metav1.Time, duration time.Duration, expect ...metav1.Condition) {
	ginkgo.By(fmt.Sprintf("Check ClusterManagementAddOn %s no conditions in duration %v", name, duration))

	gomega.Consistently(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for i, ec := range expect {
			cond := meta.FindStatusCondition(actual.Status.InstallProgressions[i].Conditions, ec.Type)

			if cond != nil &&
				cond.Status == ec.Status &&
				cond.Reason == ec.Reason &&
				cond.Message == ec.Message &&
				metav1.Now().Sub(start.Time) < duration {
				return fmt.Errorf("unexpected condition matches before duration")
			}
		}

		return nil
	}, duration, eventuallyInterval).Should(gomega.BeNil())
}

func assertManagedClusterAddOnConfigReferences(name, namespace string, expect ...addonapiv1alpha1.ConfigReference) {
	ginkgo.By(fmt.Sprintf("Check ManagedClusterAddOn %s/%s ConfigReferences", namespace, name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Get(context.Background(), name, metav1.GetOptions{})
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
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertManagedClusterAddOnConditions(name, namespace string, expect ...metav1.Condition) {
	ginkgo.By(fmt.Sprintf("Check ManagedClusterAddOn %s/%s Conditions", namespace, name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Get(context.Background(), name, metav1.GetOptions{})
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
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}
