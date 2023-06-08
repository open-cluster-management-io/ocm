package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	helloWorldTemplateAddonName = "hello-template"
)

var _ = ginkgo.Describe("install/uninstall hello template addons", func() {
	var templateAddon = addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: helloWorldTemplateAddonName,
		},
		Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: addonInstallNamespace,
		},
	}

	ginkgo.BeforeEach(func() {
		gomega.Eventually(func() error {
			_, err := hubClusterClient.ClusterV1().ManagedClusters().Get(
				context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, err = hubKubeClient.CoreV1().Namespaces().Get(
				context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloWorldTemplateAddonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(
						context.Background(), &templateAddon, metav1.CreateOptions{})
					if err != nil {
						return err
					}
				}
				return err
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		gomega.Eventually(func() error {
			_, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloWorldTemplateAddonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					// only return nil if the addon is deleted
					return nil
				}
				return err
			}

			err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(
				context.Background(), helloWorldTemplateAddonName, metav1.DeleteOptions{})
			if err != nil {
				return err
			}

			return fmt.Errorf("addon %s/%s is not deleted", managedClusterName, helloWorldTemplateAddonName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("addon should be available", func() {
		ginkgo.By("Make sure addon is available")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloWorldTemplateAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("addon should be applied to spoke, but get condition %v", addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on spoke, but get condition %v", addon.Status.Conditions)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is functioning")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: managedClusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(
			context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("The pre-delete job should clean up the configmap after the addon is deleted")
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(
			context.Background(), helloWorldTemplateAddonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			_, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the configmap should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloWorldTemplateAddonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the managedClusterAddon should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("The pre-delete job should be deleted ")
		gomega.Eventually(func() error {
			_, err := hubKubeClient.BatchV1().Jobs(addonInstallNamespace).Get(
				context.Background(), "hello-template-cleanup-configmap", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the job should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("The deployment with deletion-orphan annotation should not be deleted ")
		gomega.Eventually(func() error {
			_, err := hubKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				context.Background(), "hello-template-agent", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("the deployment should not be deleted")
				}
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("addon should be configured by addon deployment config for image override", func() {
		ginkgo.By("Prepare a AddOnDeploymentConfig for addon image override config")
		gomega.Eventually(func() error {
			return prepareImageOverrideAddOnDeploymentConfig(managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), helloWorldTemplateAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: managedClusterName,
						Name:      deployImageOverrideConfigName,
					},
				},
			}
			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := hubKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				context.Background(), "hello-template-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			containers := agentDeploy.Spec.Template.Spec.Containers
			if len(containers) != 1 {
				return fmt.Errorf("expect one container, but %v", containers)
			}

			if containers[0].Image != overrideImageValue {
				return fmt.Errorf("unexpected image %s", containers[0].Image)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
