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
	helloWorldHostedAddonName = "helloworldhosted"

	eventuallyTimeout  = 300 // seconds
	eventuallyInterval = 1   // seconds
)

var _ = ginkgo.Describe("install/uninstall helloworld hosted addons in Hosted mode", func() {
	var addonAgentNamespace string
	ginkgo.BeforeEach(func() {
		addonAgentNamespace = fmt.Sprintf("klusterlet-%s-agent-addon", hostingClusterName)

		_, err := hubClusterClient.ClusterV1().ManagedClusters().Get(
			context.Background(), hostedManagedClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = hubKubeClient.CoreV1().Namespaces().Get(
			context.Background(), hostedManagedClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = hubClusterClient.ClusterV1().ManagedClusters().Get(
			context.Background(), hostingClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = hubKubeClient.CoreV1().Namespaces().Get(
			context.Background(), hostingClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Install/uninstall addon in hosted mode and make sure it is available", func() {
		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(hostedManagedClusterName).Get(
				context.Background(), helloWorldHostedAddonName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					_, cerr := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(hostedManagedClusterName).Create(
						context.Background(),
						&addonapiv1alpha1.ManagedClusterAddOn{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: hostedManagedClusterName,
								Name:      helloWorldHostedAddonName,
								Annotations: map[string]string{
									constants.HostingClusterNameAnnotationKey: hostingClusterName,
								},
							},
							Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
								InstallNamespace: addonAgentNamespace,
							},
						},
						metav1.CreateOptions{})
					if cerr != nil {
						return cerr
					}
				}
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, constants.HostingClusterValidity) {
				return fmt.Errorf("addon hosting cluster should be valid, but get condition %v",
					addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// provide the managed cluster kubeconfig
		klusterletSecret, err := hubKubeClient.CoreV1().Secrets(hostedKlusterletName).Get(
			context.Background(), "external-managed-kubeconfig", metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addonSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: addonAgentNamespace,
				Name:      fmt.Sprintf("%s-managed-kubeconfig", helloWorldHostedAddonName),
			},
			Data: klusterletSecret.Data,
		}
		_, err = hubKubeClient.CoreV1().Secrets(addonAgentNamespace).Create(
			context.Background(), &addonSecret, metav1.CreateOptions{})
		if err != nil {
			gomega.Expect(errors.IsAlreadyExists(err)).To(gomega.BeTrue())
		}

		gomega.Eventually(func() error {
			addon, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(hostedManagedClusterName).Get(
				context.Background(), helloWorldHostedAddonName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(addon.Status.Conditions, constants.AddonHostingManifestApplied) {
				return fmt.Errorf("addon should be applied to hosting cluster, but get condition %v",
					addon.Status.Conditions)
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("addon should be available on hosting cluster, but get condition %v",
					addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Ensure helloworld addo is functioning.
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: hostedManagedClusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err = hubKubeClient.CoreV1().ConfigMaps(hostedManagedClusterName).Create(
			context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			// the configmap should be copied to the managed cluster
			copyiedConfig, err := hostedManagedKubeClient.CoreV1().ConfigMaps(addonAgentNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// remove addon
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(hostedManagedClusterName).Delete(
			context.Background(), helloWorldHostedAddonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			_, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(hostedManagedClusterName).Get(
				context.Background(), helloWorldHostedAddonName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})
})
