package e2e

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1apha1 "open-cluster-management.io/api/cluster/v1alpha1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/addon/templateagent"
	"open-cluster-management.io/ocm/test/e2e/manifests"
)

const (
	nodePlacementDeploymentConfigName = "node-placement-deploy-config"
	imageOverrideDeploymentConfigName = "image-override-deploy-config"
	originalImageValue                = "quay.io/open-cluster-management/addon-examples:latest"
	overrideImageValue                = "quay.io/ocm/addon-examples:latest"
	customSignerName                  = "example.com/signer-name"
	customSignerSecretName            = "addon-signer-secret"
)

var (
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
	registries   = []addonapiv1alpha1.ImageMirror{
		{
			Source: "quay.io/open-cluster-management/addon-examples",
			Mirror: "quay.io/ocm/addon-examples",
		},
	}
)

var _ = ginkgo.Describe("Enable addon management feature gate", ginkgo.Label("addon-manager"), func() {
	addOnName := "hello-template"
	var klusterletName, clusterName, agentNamespace, addonInstallNamespace string

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = clusterv1apha1.Install(s)
	_ = addonapiv1alpha1.Install(s)

	templateResources := []string{
		"addon/addon_deployment_config.yaml",
		"addon/addon_template.yaml",
		"addon/cluster_management_addon.yaml",
		"addon/cluster_role.yaml",
		"addon/signca_secret_role.yaml",
		"addon/signca_secret_rolebinding.yaml",
	}

	ginkgo.BeforeEach(func() {
		surfix := rand.String(6)
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", surfix)
		clusterName = fmt.Sprintf("e2e-managedcluster-%s", surfix)
		agentNamespace = fmt.Sprintf("open-cluster-management-agent-%s", surfix)
		addonInstallNamespace = fmt.Sprintf("%s-addon", agentNamespace)

		ginkgo.By("create addon custom sign secret")
		err := copySignerSecret(context.TODO(), t.HubKubeClient, "open-cluster-management-hub",
			"signer-secret", templateagent.AddonManagerNamespace(), customSignerSecretName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		// enable addon management feature gate
		gomega.Eventually(func() error {
			clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), "cluster-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.AddOnManagerConfiguration = &operatorapiv1.AddOnManagerConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "AddonManagement",
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			}
			_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		// the addon manager deployment should be running
		gomega.Eventually(t.CheckHubReady, t.EventuallyTimeout, t.EventuallyInterval).Should(gomega.Succeed())

		_, err = t.CreateApprovedKlusterlet(
			klusterletName, clusterName, agentNamespace, operatorapiv1.InstallMode(klusterletDeployMode))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("create addon template resources for cluster %v", clusterName))
		err = createResourcesFromYamlFiles(context.Background(), t.HubDynamicClient, t.hubRestMapper, s,
			defaultAddonTemplateReaderManifestsFunc(manifests.AddonManifestFiles, map[string]interface{}{
				"Namespace":              clusterName,
				"AddonInstallNamespace":  addonInstallNamespace,
				"CustomSignerName":       customSignerName,
				"AddonManagerNamespace":  templateagent.AddonManagerNamespace(),
				"CustomSignerSecretName": customSignerSecretName,
			}),
			templateResources,
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, clusterName))
		err = t.CreateManagedClusterAddOn(clusterName, addOnName, addonInstallNamespace)
		if err != nil {
			klog.Errorf("failed to create managed cluster addon %v on the managed cluster namespace %v: %v", addOnName, clusterName, err)
			gomega.Expect(errors.IsAlreadyExists(err)).To(gomega.BeTrue())
		}

		ginkgo.By(fmt.Sprintf("wait the addon %v/%v available condition to be true", clusterName, addOnName))
		gomega.Eventually(func() error {
			return t.CheckManagedClusterAddOnStatus(clusterName, addOnName)
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		ginkgo.By(fmt.Sprintf("delete the addon %v on the managed cluster namespace %v", addOnName, clusterName))
		err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(
			context.TODO(), addOnName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.Fail(fmt.Sprintf("failed to delete managed cluster addon %v on cluster %v: %v", addOnName, clusterName, err))
		}

		gomega.Eventually(func() error {
			_, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the managedClusterAddon should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("delete addon template resources for cluster %v", clusterName))
		err = deleteResourcesFromYamlFiles(context.Background(), t.HubDynamicClient, t.hubRestMapper, s,
			defaultAddonTemplateReaderManifestsFunc(manifests.AddonManifestFiles, map[string]interface{}{
				"Namespace":              clusterName,
				"AddonInstallNamespace":  addonInstallNamespace,
				"CustomSignerName":       customSignerName,
				"AddonManagerNamespace":  templateagent.AddonManagerNamespace(),
				"CustomSignerSecretName": customSignerSecretName,
			}),
			templateResources,
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("delete addon custom sign secret")
		err = t.HubKubeClient.CoreV1().Secrets(templateagent.AddonManagerNamespace()).Delete(context.TODO(),
			customSignerSecretName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.Fail(fmt.Sprintf("failed to delete custom signer secret %v/%v: %v",
				templateagent.AddonManagerNamespace(), customSignerSecretName, err))
		}

		ginkgo.By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
		gomega.Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(gomega.BeNil())

		// disable addon management feature gate
		gomega.Eventually(func() error {
			clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), "cluster-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.AddOnManagerConfiguration = &operatorapiv1.AddOnManagerConfiguration{}
			_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

	})

	ginkgo.It("Template type addon should be functioning", func() {
		ginkgo.By("Check hub kubeconfig secret is created")
		gomega.Eventually(func() error {
			_, err := t.HubKubeClient.CoreV1().Secrets(addonInstallNamespace).Get(context.TODO(),
				templateagent.HubKubeconfigSecretName(addOnName), metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Check custom signer secret is created")
		gomega.Eventually(func() error {
			_, err := t.HubKubeClient.CoreV1().Secrets(addonInstallNamespace).Get(context.TODO(),
				templateagent.CustomSignedSecretName(addOnName, customSignerName), metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout, t.EventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Make sure addon is functioning")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: clusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err := t.HubKubeClient.CoreV1().ConfigMaps(clusterName).Create(
			context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := t.SpokeKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("delete the addon %v on the managed cluster namespace %v", addOnName, clusterName))
		err = t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(
			context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("The pre-delete job should clean up the configmap after the addon is deleted")
		gomega.Eventually(func() error {
			_, err := t.SpokeKubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
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
			_, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
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
			_, err := t.SpokeKubeClient.BatchV1().Jobs(addonInstallNamespace).Get(
				context.Background(), "hello-template-cleanup-configmap", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the job should be deleted")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	})

	ginkgo.It("Template type addon should be configured by addon deployment config for image override", func() {
		ginkgo.By("Prepare a AddOnDeploymentConfig for addon image override config")
		gomega.Eventually(func() error {
			return prepareImageOverrideAddOnDeploymentConfig(clusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.Background(), addOnName, metav1.GetOptions{})
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
						Namespace: clusterName,
						Name:      imageOverrideDeploymentConfigName,
					},
				},
			}
			_, err = t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := t.SpokeKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
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

		// restore the image override config, because the override image is not available
		// but it is needed by the pre-delete job
		ginkgo.By("Restore the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{}
			_, err = t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon config is restored")
		gomega.Eventually(func() error {
			agentDeploy, err := t.SpokeKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				context.Background(), "hello-template-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			containers := agentDeploy.Spec.Template.Spec.Containers
			if len(containers) != 1 {
				return fmt.Errorf("expect one container, but %v", containers)
			}

			if containers[0].Image != originalImageValue {
				return fmt.Errorf("unexpected image %s", containers[0].Image)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Template type addon should be configured by addon deployment config for node placement", func() {
		ginkgo.By("Prepare a AddOnDeploymentConfig for addon image override config")
		gomega.Eventually(func() error {
			return prepareNodePlacementAddOnDeploymentConfig(clusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.Background(), addOnName, metav1.GetOptions{})
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
						Namespace: clusterName,
						Name:      nodePlacementDeploymentConfigName,
					},
				},
			}
			_, err = t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := t.SpokeKubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				context.Background(), "hello-template-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(agentDeploy.Spec.Template.Spec.NodeSelector, nodeSelector) {
				return fmt.Errorf("unexpected nodeSeletcor %v", agentDeploy.Spec.Template.Spec.NodeSelector)
			}

			if !equality.Semantic.DeepEqual(agentDeploy.Spec.Template.Spec.Tolerations, tolerations) {
				return fmt.Errorf("unexpected tolerations %v", agentDeploy.Spec.Template.Spec.Tolerations)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	})
})

func prepareImageOverrideAddOnDeploymentConfig(namespace string) error {
	_, err := t.AddOnClinet.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), imageOverrideDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := t.AddOnClinet.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      imageOverrideDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					Registries: registries,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	if err != nil {
		return err
	}

	return nil
}

func prepareNodePlacementAddOnDeploymentConfig(namespace string) error {
	_, err := t.AddOnClinet.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), nodePlacementDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := t.AddOnClinet.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nodePlacementDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					NodePlacement: &addonapiv1alpha1.NodePlacement{
						NodeSelector: nodeSelector,
						Tolerations:  tolerations,
					},
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	if err != nil {
		return err
	}

	return nil
}

func copySignerSecret(ctx context.Context, kubeClient kubernetes.Interface, srcNs, srcName, dstNs, dstName string) error {
	src, err := kubeClient.CoreV1().Secrets(srcNs).Get(context.Background(), srcName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dstName,
			Namespace: dstNs,
		},
		Data:       src.Data,
		StringData: src.StringData,
		Type:       src.Type,
	}

	_, err = kubeClient.CoreV1().Secrets(dstNs).Create(ctx, dst, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
