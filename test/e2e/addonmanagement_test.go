package e2e

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/valyala/fasttemplate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/addon/templateagent"
	"open-cluster-management.io/ocm/test/e2e/manifests"
)

const (
	nodePlacementDeploymentConfigName        = "node-placement-deploy-config"
	imageOverrideDeploymentConfigName        = "image-override-deploy-config"
	namespaceOverrideConfigName              = "namespace-override-config"
	proxyDeploymentConfigName                = "proxy-deploy-config"
	resourceRequirementsDeploymentConfigName = "resource-requirements-deploy-config"
	originalImageValue                       = "quay.io/open-cluster-management/addon-examples:latest"
	overrideImageValue                       = "quay.io/ocm/addon-examples:latest"
	customSignerName                         = "example.com/signer-name"
	// #nosec G101
	customSignerSecretName = "addon-signer-secret"
)

var (
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
	affinity     = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 80,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "node-role.kubernetes.io/infra", Operator: corev1.NodeSelectorOpExists},
						},
					},
				},
			},
		},
	}
	registries = []addonapiv1beta1.ImageMirror{
		{
			Source: "quay.io/open-cluster-management/addon-examples",
			Mirror: "quay.io/ocm/addon-examples",
		},
	}
	proxyConfig = addonapiv1beta1.ProxyConfig{
		HTTPProxy:  "http://proxy.example.com",
		HTTPSProxy: "http://proxy.example.com",
		NoProxy:    "localhost",
		CABundle:   []byte("test-ca-bundle"),
	}
	resourceRequirementsConfig = []addonapiv1beta1.ContainerResourceRequirements{
		{
			ContainerID: "*:*:helloworld-agent",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
			},
		},
	}
)

var _ = Describe("Addon management (v1beta1)", Ordered, Label("addon-manager"), func() {
	addOnName := "hello-template"
	addonInstallNamespace := "test-addon-template"

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = clusterv1.Install(s)
	_ = addonapiv1beta1.Install(s)

	templateResources := []string{
		"addon/addon_template.yaml",
		"addon/cluster_management_addon.yaml",
		"addon/cluster_role.yaml",
		"addon/signca_secret_role.yaml",
		"addon/signca_secret_rolebinding.yaml",
	}

	var signerSecretNamespace string

	BeforeEach(func(ctx SpecContext) {
		signerSecretNamespace = "signer-secret-test-ns-" + rand.String(6)

		By("create addon custom sign secret namespace")
		_, err := hub.KubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: signerSecretNamespace,
			},
		}, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("create addon custom sign secret")
		err = copySignerSecret(ctx, hub.KubeClient, "open-cluster-management-hub",
			"signer-secret", signerSecretNamespace, customSignerSecretName)
		Expect(err).ToNot(HaveOccurred())

		// the addon manager deployment should be running
		Eventually(func() error {
			return hub.CheckHubReady()
		}).Should(Succeed())

		By(fmt.Sprintf("create addon template resources for cluster %v", universalClusterName))
		err = createResourcesFromYamlFiles(ctx, hub.DynamicClient, hub.RestMapper, s,
			defaultAddonTemplateReaderManifestsFunc(manifests.AddonManifestFiles, map[string]interface{}{
				"Namespace":                   universalClusterName,
				"AddonInstallNamespace":       addonInstallNamespace,
				"CustomSignerName":            customSignerName,
				"AddonManagerNamespace":       templateagent.AddonManagerNamespace(),
				"CustomSignerSecretName":      customSignerSecretName,
				"CustomSignerSecretNamespace": signerSecretNamespace,
			}),
			templateResources,
		)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err = hub.CreateManagedClusterAddOnV1Beta1(universalClusterName, addOnName, addonInstallNamespace)
		if err != nil {
			klog.Errorf("failed to create managed cluster addon %v on the managed cluster namespace %v: %v", addOnName, universalClusterName, err)
			Expect(errors.IsAlreadyExists(err)).To(BeTrue())
		}

		By(fmt.Sprintf("wait the addon %v/%v available condition to be true", universalClusterName, addOnName))
		Eventually(func() error {
			return hub.CheckManagedClusterAddOnStatusV1Beta1(universalClusterName, addOnName)
		}).Should(Succeed())
	})

	AfterEach(func(ctx SpecContext) {
		By(fmt.Sprintf("delete the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Delete(
			ctx, addOnName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			Fail(fmt.Sprintf("failed to delete managed cluster addon %v on cluster %v: %v", addOnName, universalClusterName, err))
		}

		Eventually(func() error {
			_, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err == nil {
				return fmt.Errorf("the managedClusterAddon %s should be deleted", addOnName)
			}
			if err != nil && !errors.IsNotFound(err) {
				return err
			}

			// check works after addon is not found
			works, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).List(
				ctx, metav1.ListOptions{})
			if err == nil && len(works.Items) != 0 {
				return fmt.Errorf("expected no works,but got: %+v", works.Items)
			}
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By(fmt.Sprintf("delete addon template resources for cluster %v", universalClusterName))
		err = deleteResourcesFromYamlFiles(ctx, hub.DynamicClient, hub.RestMapper, s,
			defaultAddonTemplateReaderManifestsFunc(manifests.AddonManifestFiles, map[string]interface{}{
				"Namespace":                   universalClusterName,
				"AddonInstallNamespace":       addonInstallNamespace,
				"CustomSignerName":            customSignerName,
				"AddonManagerNamespace":       templateagent.AddonManagerNamespace(),
				"CustomSignerSecretName":      customSignerSecretName,
				"CustomSignerSecretNamespace": signerSecretNamespace,
			}),
			templateResources,
		)
		Expect(err).ToNot(HaveOccurred())

		By("delete addon custom sign secret")
		err = hub.KubeClient.CoreV1().Secrets(signerSecretNamespace).Delete(ctx,
			customSignerSecretName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			Fail(fmt.Sprintf("failed to delete custom signer secret %v/%v: %v",
				signerSecretNamespace, customSignerSecretName, err))
		}

		By("delete addon custom sign secret namespace")
		err = hub.KubeClient.CoreV1().Namespaces().Delete(ctx, signerSecretNamespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			Fail(fmt.Sprintf("failed to delete custom signer secret namespace %v: %v", signerSecretNamespace, err))
		}

		// delete all CSR created for the addon on the hub cluster, otherwise if it reaches the limit number 10, the
		// other tests will fail
		By(fmt.Sprintf("delete CSRs for addon %v on cluster %v", addOnName, universalClusterName))
		Eventually(func() error {
			listOpts := metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s,%s=%s", addonapiv1beta1.AddonLabelKey, addOnName,
					clusterv1.ClusterNameLabelKey, universalClusterName),
			}

			csrs, err := hub.KubeClient.CertificatesV1().CertificateSigningRequests().List(ctx, listOpts)
			if err != nil {
				return err
			}

			if len(csrs.Items) > 0 {
				klog.Infof("Found %d CSRs to delete:", len(csrs.Items))
				err := hub.KubeClient.CertificatesV1().CertificateSigningRequests().DeleteCollection(ctx,
					metav1.DeleteOptions{}, listOpts)
				if err != nil && !errors.IsNotFound(err) {
					return err
				}
				return fmt.Errorf("waiting for %d CSRs to be fully deleted", len(csrs.Items))
			}

			klog.Infof("All CSRs deleted successfully")
			return nil
		}).ShouldNot(HaveOccurred())
	})

	It("Template type addon should be functioning", func(ctx SpecContext) {
		By("Check hub kubeconfig secret is created")
		Eventually(func() error {
			_, err := hub.KubeClient.CoreV1().Secrets(addonInstallNamespace).Get(ctx,
				templateagent.HubKubeconfigSecretName(addOnName), metav1.GetOptions{})
			return err
		}).Should(Succeed())

		By("Check custom client cert secret is created")
		Eventually(func() error {
			_, err := hub.KubeClient.CoreV1().Secrets(addonInstallNamespace).Get(ctx,
				templateagent.CustomSignedSecretName(addOnName, customSignerName), metav1.GetOptions{})
			return err
		}).Should(Succeed())

		By("Make sure addon is functioning")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: universalClusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err := hub.KubeClient.CoreV1().ConfigMaps(universalClusterName).Create(
			ctx, configmap, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			copiedConfig, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				ctx, configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(copiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copiedConfig.Data)
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Make sure manifestwork config is configured")
		manifestWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(ctx,
			fmt.Sprintf("addon-%s-deploy-0", addOnName), metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		foundDeploymentConfig := false
		expectedDeploymentResourceIdentifier := workapiv1.ResourceIdentifier{
			Group:     "apps",
			Resource:  "deployments",
			Name:      "hello-template-agent",
			Namespace: addonInstallNamespace,
		}
		foundDaemonSetConfig := false
		expectedDaemonSetResourceIdentifier := workapiv1.ResourceIdentifier{
			Group:     "apps",
			Resource:  "daemonsets",
			Name:      "hello-template-agent-ds",
			Namespace: addonInstallNamespace,
		}
		for _, mc := range manifestWork.Spec.ManifestConfigs {
			if mc.ResourceIdentifier == expectedDeploymentResourceIdentifier {
				foundDeploymentConfig = true
				Expect(mc.UpdateStrategy.Type).To(Equal(workapiv1.UpdateStrategyTypeServerSideApply))
			}
			if mc.ResourceIdentifier == expectedDaemonSetResourceIdentifier {
				foundDaemonSetConfig = true
			}
		}
		if !foundDeploymentConfig || !foundDaemonSetConfig {
			Expect(fmt.Errorf("expected manifestwork is not correct, %v",
				manifestWork.Spec.ManifestConfigs)).ToNot(HaveOccurred())
		}

		By(fmt.Sprintf("delete the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Delete(
			ctx, addOnName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("The pre-delete job should clean up the configmap after the addon is deleted")
		Eventually(func() error {
			_, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				ctx, configmap.Name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the configmap should be deleted")
		}).ShouldNot(HaveOccurred())

		Eventually(func() error {
			_, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the managedClusterAddon should be deleted")
		}).ShouldNot(HaveOccurred())

		By("The pre-delete job should be deleted ")
		Eventually(func() error {
			_, err := spoke.KubeClient.BatchV1().Jobs(addonInstallNamespace).Get(
				ctx, "hello-template-cleanup-configmap", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the job should be deleted")
		}).ShouldNot(HaveOccurred())
	})

	It("Template type addon should be configured by addon deployment config for image override"+
		"even there are cluster annotation config", func(ctx SpecContext) {
		By("Prepare cluster annotation for addon image override config")
		overrideRegistries := addonapiv1beta1.AddOnDeploymentConfigSpec{
			// should be different from the registries in the addonDeploymentConfig
			Registries: []addonapiv1beta1.ImageMirror{
				{
					Source: "quay.io/open-cluster-management/addon-examples",
					Mirror: "quay.io/ocm/addon-examples-test",
				},
			},
		}
		registriesJson, err := json.Marshal(overrideRegistries)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				ctx, universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			newCluster := cluster.DeepCopy()

			annotations := cluster.Annotations
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[clusterv1.ClusterImageRegistriesAnnotationKey] = string(registriesJson)

			newCluster.Annotations = annotations
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(
				ctx, newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(HaveOccurred())

		By("Prepare a AddOnDeploymentConfig for addon image override config")
		Eventually(func() error {
			return prepareImageOverrideAddOnDeploymentConfig(ctx, universalClusterName, addonInstallNamespace)
		}).ShouldNot(HaveOccurred())

		By("Add the configs to ManagedClusterAddOn")
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1beta1.ConfigReferent{
						Namespace: universalClusterName,
						Name:      imageOverrideDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Update(
				ctx, newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Make sure addon is configured")
		Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
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
		}).ShouldNot(HaveOccurred())

		By("Restore the managed cluster annotation")
		Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				ctx, universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			newCluster := cluster.DeepCopy()
			delete(newCluster.Annotations, clusterv1.ClusterImageRegistriesAnnotationKey)
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(
				ctx, newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(HaveOccurred())

		// restore the image override config, because the override image is not available
		// but it is needed by the pre-delete job
		By("Restore the configs to ManagedClusterAddOn")
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1beta1.AddOnConfig{}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Update(
				ctx, newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Make sure addon config is restored")
		Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
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
		}).ShouldNot(HaveOccurred())
	})

	It("Template type addon should be configured by addon deployment config for node placement", func(ctx SpecContext) {
		By("Prepare a AddOnDeploymentConfig for addon image override config")
		Eventually(func() error {
			return prepareNodePlacementAddOnDeploymentConfig(ctx, universalClusterName, addonInstallNamespace)
		}).ShouldNot(HaveOccurred())

		By("Add the configs to ManagedClusterAddOn")
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1beta1.ConfigReferent{
						Namespace: universalClusterName,
						Name:      nodePlacementDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Update(
				ctx, newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Make sure addon is configured")
		Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(agentDeploy.Spec.Template.Spec.NodeSelector, nodeSelector) {
				return fmt.Errorf("unexpected nodeSeletcor %v", agentDeploy.Spec.Template.Spec.NodeSelector)
			}

			if !equality.Semantic.DeepEqual(agentDeploy.Spec.Template.Spec.Tolerations, tolerations) {
				return fmt.Errorf("unexpected tolerations %v", agentDeploy.Spec.Template.Spec.Tolerations)
			}

			if !equality.Semantic.DeepEqual(agentDeploy.Spec.Template.Spec.Affinity, affinity) {
				return fmt.Errorf("unexpected affinity %v", agentDeploy.Spec.Template.Spec.Affinity)
			}

			return nil
		}).ShouldNot(HaveOccurred())

	})

	It("Template type addon should be configured by addon deployment config for namespace", func(ctx SpecContext) {
		By("Prepare a AddOnDeploymentConfig for namespace config")
		overrideNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "another-addon-namespace",
			},
		}
		_, err := spoke.KubeClient.CoreV1().Namespaces().Create(ctx, overrideNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() error {
			return prepareInstallNamespace(ctx, universalClusterName, overrideNamespace.Name)
		}).ShouldNot(HaveOccurred())

		By("Add the configs to ManagedClusterAddOn")
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1beta1.ConfigReferent{
						Namespace: universalClusterName,
						Name:      namespaceOverrideConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Update(
				ctx, newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Make sure addon is configured")
		Eventually(func() error {
			_, err := spoke.KubeClient.AppsV1().Deployments(overrideNamespace.Name).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
			return err
		}).ShouldNot(HaveOccurred())
	})

	It("Template type addon's image should be overrode by cluster annotation", func(ctx SpecContext) {
		By("Prepare cluster annotation for addon image override config")
		overrideRegistries := addonapiv1beta1.AddOnDeploymentConfigSpec{
			Registries: registries,
		}
		registriesJson, err := json.Marshal(overrideRegistries)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				ctx, universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			newCluster := cluster.DeepCopy()

			annotations := cluster.Annotations
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[clusterv1.ClusterImageRegistriesAnnotationKey] = string(registriesJson)

			newCluster.Annotations = annotations
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(
				ctx, newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(HaveOccurred())

		By("Make sure addon is configured")
		Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
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
		}).ShouldNot(HaveOccurred())

		// restore the image override config, because the override image is not available
		// but it is needed by the pre-delete job
		By("Restore the managed cluster annotation")
		Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				ctx, universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			newCluster := cluster.DeepCopy()
			delete(newCluster.Annotations, clusterv1.ClusterImageRegistriesAnnotationKey)
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(
				ctx, newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(HaveOccurred())

		By("Make sure addon config is restored")
		Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
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
		}).ShouldNot(HaveOccurred())
	})

	It("Template type addon should be configured by addon deployment config for proxy", func(ctx SpecContext) {
		By("Prepare a AddOnDeploymentConfig for addon proxy config")
		Eventually(func() error {
			return prepareProxyAddOnDeploymentConfig(ctx, universalClusterName, addonInstallNamespace)
		}).ShouldNot(HaveOccurred())

		By("Add the configs to ManagedClusterAddOn")
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1beta1.ConfigReferent{
						Namespace: universalClusterName,
						Name:      proxyDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Update(
				ctx, newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Make sure addon is configured")
		Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			for _, container := range agentDeploy.Spec.Template.Spec.Containers {
				found := 0
				for _, env := range container.Env {
					if env.Name == "HTTP_PROXY" || env.Name == "http_proxy" {
						if env.Value != proxyConfig.HTTPProxy {
							return fmt.Errorf("unexpected HTTP_PROXY %s", env.Value)
						}
						found++
					}
					if env.Name == "HTTPS_PROXY" || env.Name == "https_proxy" {
						if env.Value != proxyConfig.HTTPSProxy {
							return fmt.Errorf("unexpected HTTPS_PROXY %s", env.Value)
						}
						found++
					}
					if env.Name == "NO_PROXY" || env.Name == "no_proxy" {
						if env.Value != proxyConfig.NoProxy {
							return fmt.Errorf("unexpected NO_PROXY %s", env.Value)
						}
						found++
					}
					if env.Name == "CA_BUNDLE_FILE_PATH" {
						if env.Value != "/managed/proxy-ca/ca-bundle.crt" {
							return fmt.Errorf("unexpected CA_BUNDLE_FILE_PATH %s", env.Value)
						}
						found++
					}
				}
				if found != 7 {
					return fmt.Errorf("unexpected env %v", container.Env)
				}
			}

			return nil
		}).ShouldNot(HaveOccurred())

		cm, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(ctx,
			fmt.Sprintf("%s-proxy-ca", addOnName), metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(cm.Data).To(HaveKey("ca-bundle.crt"))
		Expect(cm.Data["ca-bundle.crt"]).To(Equal(string(proxyConfig.CABundle)))
	})

	It("Template type addon should be configured by addon deployment config for resource requirement", func(ctx SpecContext) {
		By("Prepare a AddOnDeploymentConfig for addon resource requirement config")
		Eventually(func() error {
			return prepareResourceRequirementsAddOnDeploymentConfig(ctx, universalClusterName, addonInstallNamespace)
		}).ShouldNot(HaveOccurred())

		By("Add the configs to ManagedClusterAddOn")
		Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1beta1.ConfigReferent{
						Namespace: universalClusterName,
						Name:      resourceRequirementsDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Update(
				ctx, newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Make sure addon is configured")
		Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				ctx, "hello-template-agent", metav1.GetOptions{})
			if err != nil {
				return err
			}

			for _, container := range agentDeploy.Spec.Template.Spec.Containers {
				if container.Name == "helloworld-agent" {
					if !equality.Semantic.DeepEqual(container.Resources, resourceRequirementsConfig[0].Resources) {
						return fmt.Errorf("unexpected resource requirements for deployment: %v", container.Resources)
					}
				}
			}

			agentDaemonset, err := spoke.KubeClient.AppsV1().DaemonSets(addonInstallNamespace).Get(
				ctx, "hello-template-agent-ds", metav1.GetOptions{})
			if err != nil {
				return err
			}
			for _, container := range agentDaemonset.Spec.Template.Spec.Containers {
				if container.Name == "helloworld-agent" {
					if !equality.Semantic.DeepEqual(container.Resources, resourceRequirementsConfig[0].Resources) {
						return fmt.Errorf("unexpected resource requirements for daemonset: %v", container.Resources)
					}
				}
			}
			return nil
		}).ShouldNot(HaveOccurred())
	})

	It("ClusterManagementAddon deletion should wait for ManagedClusterAddons cleanup", func(ctx SpecContext) {
		By("Make sure addon is functioning before deletion")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-%s", rand.String(6)),
				Namespace: universalClusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err := hub.KubeClient.CoreV1().ConfigMaps(universalClusterName).Create(
			ctx, configmap, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			copiedConfig, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				ctx, configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(copiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copiedConfig.Data)
			}
			return nil
		}).ShouldNot(HaveOccurred())

		By("Delete the ClusterManagementAddon to trigger cascading deletion")
		err = hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(
			ctx, addOnName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("The pre-delete job should clean up the configmap")
		Eventually(func() error {
			_, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				ctx, configmap.Name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("the configmap should be deleted")
		}).ShouldNot(HaveOccurred())

		By("ManagedClusterAddon should eventually be deleted after pre-delete job completes")
		Eventually(func() error {
			_, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(universalClusterName).Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("the ManagedClusterAddon should be deleted")
		}).ShouldNot(HaveOccurred())

		By("The pre-delete job should be cleaned up")
		Eventually(func() error {
			_, err := spoke.KubeClient.BatchV1().Jobs(addonInstallNamespace).Get(
				ctx, "hello-template-cleanup-configmap", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("the job should be deleted")
		}).ShouldNot(HaveOccurred())

		By("Verify ClusterManagementAddon is deleted")
		Eventually(func() error {
			_, err := hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().Get(
				ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("the ClusterManagementAddon should be deleted")
		}).ShouldNot(HaveOccurred())
	})
})

func prepareInstallNamespace(ctx context.Context, namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Get(
		ctx, namespaceOverrideConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Create(
			ctx,
			&addonapiv1beta1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespaceOverrideConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
					AgentInstallNamespace: installNamespace,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareImageOverrideAddOnDeploymentConfig(ctx context.Context, namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Get(
		ctx, imageOverrideDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Create(
			ctx,
			&addonapiv1beta1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      imageOverrideDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
					Registries:            registries,
					AgentInstallNamespace: installNamespace,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareNodePlacementAddOnDeploymentConfig(ctx context.Context, namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Get(
		ctx, nodePlacementDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Create(
			ctx,
			&addonapiv1beta1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nodePlacementDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
					NodePlacement: &addonapiv1beta1.NodePlacement{
						NodeSelector: nodeSelector,
						Tolerations:  tolerations,
						Affinity:     affinity,
					},
					AgentInstallNamespace: installNamespace,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareProxyAddOnDeploymentConfig(ctx context.Context, namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Get(
		ctx, proxyDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Create(
			ctx,
			&addonapiv1beta1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
					NodePlacement: &addonapiv1beta1.NodePlacement{
						NodeSelector: nodeSelector,
						Tolerations:  tolerations,
					},
					AgentInstallNamespace: installNamespace,
					ProxyConfig:           proxyConfig,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func prepareResourceRequirementsAddOnDeploymentConfig(ctx context.Context, namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Get(
		ctx, resourceRequirementsDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1beta1().AddOnDeploymentConfigs(namespace).Create(
			ctx,
			&addonapiv1beta1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceRequirementsDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
					AgentInstallNamespace: installNamespace,
					ProxyConfig:           proxyConfig,
					ResourceRequirements:  resourceRequirementsConfig,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		return nil
	}

	return err
}

func copySignerSecret(ctx context.Context, kubeClient kubernetes.Interface, srcNs, srcName, dstNs, dstName string) error {
	src, err := kubeClient.CoreV1().Secrets(srcNs).Get(ctx, srcName, metav1.GetOptions{})
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

func createResourcesFromYamlFiles(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	restMapper meta.RESTMapper,
	scheme *runtime.Scheme,
	manifests func(name string) ([]byte, error),
	resourceFiles []string) error {

	var appliedErrs []error

	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()
	for _, fileName := range resourceFiles {
		objData, err := manifests(fileName)
		if err != nil {
			return err
		}
		required := unstructured.Unstructured{}
		_, gvk, err := decoder.Decode(objData, nil, &required)
		if err != nil {
			return err
		}

		mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		_, err = dynamicClient.Resource(mapping.Resource).Namespace(required.GetNamespace()).Create(
			ctx, &required, metav1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			appliedErrs = append(appliedErrs, fmt.Errorf("%q (%T): %v", fileName, mapping.Resource, err))
		}
	}

	return utilerrors.NewAggregate(appliedErrs)
}

func deleteResourcesFromYamlFiles(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	restMapper meta.RESTMapper,
	scheme *runtime.Scheme,
	manifests func(name string) ([]byte, error),
	resourceFiles []string) error {

	var appliedErrs []error

	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()
	for _, fileName := range resourceFiles {
		objData, err := manifests(fileName)
		if err != nil {
			return err
		}
		required := unstructured.Unstructured{}
		_, gvk, err := decoder.Decode(objData, nil, &required)
		if err != nil {
			return err
		}

		mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		err = dynamicClient.Resource(mapping.Resource).Namespace(required.GetNamespace()).Delete(
			ctx, required.GetName(), metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			appliedErrs = append(appliedErrs, fmt.Errorf("%q (%T): %v", fileName, mapping.Resource, err))
		}
	}

	return utilerrors.NewAggregate(appliedErrs)
}

// defaultAddonTemplateReaderManifestsFunc returns a function that reads the addon template from the embed.FS,
// and replaces the placeholder in format of "<< placeholder >>" with the value in configValues.
func defaultAddonTemplateReaderManifestsFunc(
	fs embed.FS,
	configValues map[string]interface{},
) func(string) ([]byte, error) {

	return func(fileName string) ([]byte, error) {
		template, err := fs.ReadFile(fileName)
		if err != nil {
			return nil, err
		}

		t := fasttemplate.New(string(template), "<< ", " >>")
		objData := t.ExecuteString(configValues)
		return []byte(objData), nil
	}
}
