package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	helloWorldHelmAddonName = "helloworldhelm"
	addonInstallNamespace   = "open-cluster-management-agent-addon"
)

var _ = ginkgo.Describe("install/uninstall helloworld helm addons", func() {
	var helloworldhelmAddon = addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: helloWorldHelmAddonName,
		},
		Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: addonInstallNamespace,
		},
	}
	ginkgo.BeforeEach(func() {
		gomega.Eventually(func() error {
			_, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, err = hubKubeClient.CoreV1().Namespaces().Get(context.Background(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), helloWorldHelmAddonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), &helloworldhelmAddon, metav1.CreateOptions{})
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
			_, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), helloWorldHelmAddonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), helloWorldHelmAddonName, metav1.DeleteOptions{})

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Install/uninstall addon and make sure it is available", func() {
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), helloWorldHelmAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			hasPreDeleteFinalizer := false
			for _, f := range addon.Finalizers {
				if f == constants.PreDeleteHookFinalizer {
					hasPreDeleteFinalizer = true
				}
			}
			if !hasPreDeleteFinalizer {
				return fmt.Errorf("expected pre delete hook finalizer")
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("addon should be applied to spoke, but get condition %v", addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on spoke, but get condition %v", addon.Status.Conditions)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Ensure helloworldhelm addon is functioning.
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

		_, err := hubKubeClient.CoreV1().ConfigMaps(managedClusterName).Create(context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// delete addon CR, the pre-delete job should clean up the configmap. the addon and agent should be deleted too.
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.Background(), helloWorldHelmAddonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			_, err := hubKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the configmap should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			_, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), helloWorldHelmAddonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the managedClusterAddon should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := hubKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(context.Background(), "helloworldhelm-agent", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the agent deployment should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
	ginkgo.It("values in annotation of addon cr works", func() {
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), helloWorldHelmAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.SetAnnotations(map[string]string{
				"addon.open-cluster-management.io/values": `{"global":{"imagePullSecret":"mySecret","imageOverrides":{"helloWorldHelm":"quay.io/test:test"}}}`,
			})
			_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			agentDeploy, err := hubKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(context.Background(), "helloworldhelm-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}
			imagePullSecrets := agentDeploy.Spec.Template.Spec.ImagePullSecrets
			if len(imagePullSecrets) != 1 {
				return fmt.Errorf("the imagePullSecret is not overriden by the value in annotion")
			}
			if imagePullSecrets[0].Name != "mySecret" {
				return fmt.Errorf("the imagePullSecret is not overriden by the value in annotion")
			}
			container := agentDeploy.Spec.Template.Spec.Containers[0]
			if container.Image != "quay.io/test:test" {
				return fmt.Errorf("the image is not overriden by the values in annotion")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

})
