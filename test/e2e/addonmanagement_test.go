package e2e

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
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
	registries   = []addonapiv1alpha1.ImageMirror{
		{
			Source: "quay.io/open-cluster-management/addon-examples",
			Mirror: "quay.io/ocm/addon-examples",
		},
	}
	proxyConfig = addonapiv1alpha1.ProxyConfig{
		HTTPProxy:  "http://proxy.example.com",
		HTTPSProxy: "http://proxy.example.com",
		NoProxy:    "localhost",
		CABundle:   []byte("test-ca-bundle"),
	}
	resourceRequirementsConfig = []addonapiv1alpha1.ContainerResourceRequirements{
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

var _ = ginkgo.Describe("Enable addon management feature gate", ginkgo.Ordered, ginkgo.Label("addon-manager"), func() {
	addOnName := "hello-template"
	addonInstallNamespace := "test-addon-template"

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = clusterv1.Install(s)
	_ = addonapiv1alpha1.Install(s)

	templateResources := []string{
		"addon/addon_template.yaml",
		"addon/cluster_management_addon.yaml",
		"addon/cluster_role.yaml",
		"addon/signca_secret_role.yaml",
		"addon/signca_secret_rolebinding.yaml",
	}

	var signerSecretNamespace string

	ginkgo.BeforeEach(func() {
		signerSecretNamespace = "signer-secret-test-ns" + rand.String(6)

		ginkgo.By("create addon custom sign secret namespace")
		_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: signerSecretNamespace,
			},
		}, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By("create addon custom sign secret")
		err = copySignerSecret(context.TODO(), hub.KubeClient, "open-cluster-management-hub",
			"signer-secret", signerSecretNamespace, customSignerSecretName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// the addon manager deployment should be running
		gomega.Eventually(func() error {
			return hub.CheckHubReady()
		}).Should(gomega.Succeed())

		ginkgo.By(fmt.Sprintf("create addon template resources for cluster %v", universalClusterName))
		err = createResourcesFromYamlFiles(context.Background(), hub.DynamicClient, hub.RestMapper, s,
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
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err = hub.CreateManagedClusterAddOn(universalClusterName, addOnName, "test-ns") // the install namespace will be ignored
		if err != nil {
			klog.Errorf("failed to create managed cluster addon %v on the managed cluster namespace %v: %v", addOnName, universalClusterName, err)
			gomega.Expect(errors.IsAlreadyExists(err)).To(gomega.BeTrue())
		}

		ginkgo.By(fmt.Sprintf("wait the addon %v/%v available condition to be true", universalClusterName, addOnName))
		gomega.Eventually(func() error {
			return hub.CheckManagedClusterAddOnStatus(universalClusterName, addOnName)
		}).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		ginkgo.By(fmt.Sprintf("delete the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Delete(
			context.TODO(), addOnName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.Fail(fmt.Sprintf("failed to delete managed cluster addon %v on cluster %v: %v", addOnName, universalClusterName, err))
		}

		gomega.Eventually(func() error {
			_, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err == nil {
				return fmt.Errorf("the managedClusterAddon %s should be deleted", addOnName)
			}
			if err != nil && !errors.IsNotFound(err) {
				return err
			}

			// check works after addon is not found
			works, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).List(
				context.TODO(), metav1.ListOptions{})
			if err == nil && len(works.Items) != 0 {
				return fmt.Errorf("expected no works,but got: %+v", works.Items)
			}
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("delete addon template resources for cluster %v", universalClusterName))
		err = deleteResourcesFromYamlFiles(context.Background(), hub.DynamicClient, hub.RestMapper, s,
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
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("delete addon custom sign secret")
		err = hub.KubeClient.CoreV1().Secrets(signerSecretNamespace).Delete(context.TODO(),
			customSignerSecretName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.Fail(fmt.Sprintf("failed to delete custom signer secret %v/%v: %v",
				signerSecretNamespace, customSignerSecretName, err))
		}

		ginkgo.By("delete addon custom sign secret namespace")
		err = hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), signerSecretNamespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.Fail(fmt.Sprintf("failed to delete custom signer secret namespace %v: %v", signerSecretNamespace, err))
		}

		// delete all CSR created for the addon on the hub cluster, otherwise if it reches the limit number 10, the
		// other tests will fail
		gomega.Eventually(func() error {
			csrs, err := hub.KubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(),
				metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s,%s=%s", addonapiv1alpha1.AddonLabelKey, addOnName,
						clusterv1.ClusterNameLabelKey, universalClusterName),
				})
			if err != nil {
				return err
			}

			for _, csr := range csrs.Items {
				err = hub.KubeClient.CertificatesV1().CertificateSigningRequests().Delete(context.TODO(),
					csr.Name, metav1.DeleteOptions{})
				if err != nil {
					return err
				}
			}

			return nil
		}).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Template type addon should be functioning", func() {
		ginkgo.By("Check hub kubeconfig secret is created")
		gomega.Eventually(func() error {
			_, err := hub.KubeClient.CoreV1().Secrets(addonInstallNamespace).Get(context.TODO(),
				templateagent.HubKubeconfigSecretName(addOnName), metav1.GetOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Check custom client cert secret is created")
		gomega.Eventually(func() error {
			_, err := hub.KubeClient.CoreV1().Secrets(addonInstallNamespace).Get(context.TODO(),
				templateagent.CustomSignedSecretName(addOnName, customSignerName), metav1.GetOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Make sure addon is functioning")
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
			context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			copyiedConfig, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(copyiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copyiedConfig.Data)
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure manifestwork config is configured")
		manifestWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(),
			fmt.Sprintf("addon-%s-deploy-0", addOnName), metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
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
				gomega.Expect(mc.UpdateStrategy.Type).To(gomega.Equal(workapiv1.UpdateStrategyTypeServerSideApply))
			}
			if mc.ResourceIdentifier == expectedDaemonSetResourceIdentifier {
				foundDaemonSetConfig = true
			}
		}
		if !foundDeploymentConfig || !foundDaemonSetConfig {
			gomega.Expect(fmt.Errorf("expected manifestwork is not correct, %v",
				manifestWork.Spec.ManifestConfigs)).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By(fmt.Sprintf("delete the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Delete(
			context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("The pre-delete job should clean up the configmap after the addon is deleted")
		gomega.Eventually(func() error {
			_, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the configmap should be deleted")
		}).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the managedClusterAddon should be deleted")
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("The pre-delete job should be deleted ")
		gomega.Eventually(func() error {
			_, err := spoke.KubeClient.BatchV1().Jobs(addonInstallNamespace).Get(
				context.Background(), "hello-template-cleanup-configmap", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}

			return fmt.Errorf("the job should be deleted")
		}).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Template type addon should be configured by addon deployment config for image override"+
		"even there are cluster annotation config", func() {
		ginkgo.By("Prepare cluster annotation for addon image override config")
		overrideRegistries := addonapiv1alpha1.AddOnDeploymentConfigSpec{
			// should be different from the registries in the addonDeploymentConfig
			Registries: []addonapiv1alpha1.ImageMirror{
				{
					Source: "quay.io/open-cluster-management/addon-examples",
					Mirror: "quay.io/ocm/addon-examples-test",
				},
			},
		}
		registriesJson, err := json.Marshal(overrideRegistries)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				context.Background(), universalClusterName, metav1.GetOptions{})
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
				context.Background(), newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Prepare a AddOnDeploymentConfig for addon image override config")
		gomega.Eventually(func() error {
			return prepareImageOverrideAddOnDeploymentConfig(universalClusterName, addonInstallNamespace)
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
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
						Namespace: universalClusterName,
						Name:      imageOverrideDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
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
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Restore the managed cluster annotation")
		gomega.Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				context.Background(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			newCluster := cluster.DeepCopy()
			delete(newCluster.Annotations, clusterv1.ClusterImageRegistriesAnnotationKey)
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(
				context.Background(), newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(gomega.HaveOccurred())

		// restore the image override config, because the override image is not available
		// but it is needed by the pre-delete job
		ginkgo.By("Restore the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
				context.Background(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			newAddon := addon.DeepCopy()
			newAddon.Spec.Configs = []addonapiv1alpha1.AddOnConfig{}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon config is restored")
		gomega.Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
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
		}).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Template type addon should be configured by addon deployment config for node placement", func() {
		ginkgo.By("Prepare a AddOnDeploymentConfig for addon image override config")
		gomega.Eventually(func() error {
			return prepareNodePlacementAddOnDeploymentConfig(universalClusterName, addonInstallNamespace)
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
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
						Namespace: universalClusterName,
						Name:      nodePlacementDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
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
		}).ShouldNot(gomega.HaveOccurred())

	})

	ginkgo.It("Template type addon should be configured by addon deployment config for namespace", func() {
		ginkgo.By("Prepare a AddOnDeploymentConfig for namespace config")
		overrideNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "another-addon-namespace",
			},
		}
		_, err := spoke.KubeClient.CoreV1().Namespaces().Create(context.TODO(), overrideNamespace, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			return prepareInstallNamespace(universalClusterName, overrideNamespace.Name)
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
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
						Namespace: universalClusterName,
						Name:      namespaceOverrideConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			_, err := spoke.KubeClient.AppsV1().Deployments(overrideNamespace.Name).Get(
				context.Background(), "hello-template-agent", metav1.GetOptions{})
			return err
		}).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Template type addon's image should be overrode by cluster annotation", func() {
		ginkgo.By("Prepare cluster annotation for addon image override config")
		overrideRegistries := addonapiv1alpha1.AddOnDeploymentConfigSpec{
			Registries: registries,
		}
		registriesJson, err := json.Marshal(overrideRegistries)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				context.Background(), universalClusterName, metav1.GetOptions{})
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
				context.Background(), newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
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
		}).ShouldNot(gomega.HaveOccurred())

		// restore the image override config, because the override image is not available
		// but it is needed by the pre-delete job
		ginkgo.By("Restore the managed cluster annotation")
		gomega.Eventually(func() error {
			cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
				context.Background(), universalClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			newCluster := cluster.DeepCopy()
			delete(newCluster.Annotations, clusterv1.ClusterImageRegistriesAnnotationKey)
			_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(
				context.Background(), newCluster, metav1.UpdateOptions{})
			return err
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon config is restored")
		gomega.Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
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
		}).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Template type addon should be configured by addon deployment config for proxy", func() {
		ginkgo.By("Prepare a AddOnDeploymentConfig for addon proxy config")
		gomega.Eventually(func() error {
			return prepareProxyAddOnDeploymentConfig(universalClusterName, addonInstallNamespace)
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
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
						Namespace: universalClusterName,
						Name:      proxyDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				context.Background(), "hello-template-agent", metav1.GetOptions{})
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
		}).ShouldNot(gomega.HaveOccurred())

		cm, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(context.TODO(),
			fmt.Sprintf("%s-proxy-ca", addOnName), metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(cm.Data).To(gomega.HaveKey("ca-bundle.crt"))
		gomega.Expect(cm.Data["ca-bundle.crt"]).To(gomega.Equal(string(proxyConfig.CABundle)))
	})

	ginkgo.It("Template type addon should be configured by addon deployment config for resource requirement", func() {
		ginkgo.By("Prepare a AddOnDeploymentConfig for addon resource requirement config")
		gomega.Eventually(func() error {
			return prepareResourceRequirementsAddOnDeploymentConfig(universalClusterName, addonInstallNamespace)
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Add the configs to ManagedClusterAddOn")
		gomega.Eventually(func() error {
			addon, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
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
						Namespace: universalClusterName,
						Name:      resourceRequirementsDeploymentConfigName,
					},
				},
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Update(
				context.Background(), newAddon, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Make sure addon is configured")
		gomega.Eventually(func() error {
			agentDeploy, err := spoke.KubeClient.AppsV1().Deployments(addonInstallNamespace).Get(
				context.Background(), "hello-template-agent", metav1.GetOptions{})
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
				context.Background(), "hello-template-agent-ds", metav1.GetOptions{})
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
		}).ShouldNot(gomega.HaveOccurred())
	})
})

func prepareInstallNamespace(namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), namespaceOverrideConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespaceOverrideConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
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

func prepareImageOverrideAddOnDeploymentConfig(namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), imageOverrideDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      imageOverrideDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
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

func prepareNodePlacementAddOnDeploymentConfig(namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), nodePlacementDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Create(
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

func prepareProxyAddOnDeploymentConfig(namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), proxyDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					NodePlacement: &addonapiv1alpha1.NodePlacement{
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

func prepareResourceRequirementsAddOnDeploymentConfig(namespace, installNamespace string) error {
	_, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(
		context.Background(), resourceRequirementsDeploymentConfigName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := hub.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Create(
			context.Background(),
			&addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceRequirementsDeploymentConfigName,
					Namespace: namespace,
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
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
			fmt.Printf("Error creating %q (%T): %v\n", fileName, mapping.Resource, err)
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
			fmt.Printf("Error deleting %q (%T): %v\n", fileName, mapping.Resource, err)
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
