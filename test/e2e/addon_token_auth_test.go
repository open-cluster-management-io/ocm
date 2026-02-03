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

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/addon/templateagent"
	"open-cluster-management.io/ocm/test/e2e/manifests"
)

var _ = ginkgo.Describe("Template addon with token-based authentication", ginkgo.Ordered, ginkgo.Label("addon-manager", "addon-token-auth"), func() {
	addOnName := "hello-template"
	addonInstallNamespace := "test-addon-template-token"
	var signerSecretNamespace string
	var originalAddOnDriver *operatorapiv1.AddOnRegistrationDriver

	var agentClient kubernetes.Interface
	var agentNamespace string
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = addonapiv1alpha1.Install(s)
	_ = clusterv1.Install(s)

	templateResources := []string{
		"addon/addon_template.yaml",
		"addon/cluster_management_addon.yaml",
		"addon/cluster_role.yaml",
		"addon/signca_secret_role.yaml",
		"addon/signca_secret_rolebinding.yaml",
	}

	ginkgo.BeforeAll(func() {
		ginkgo.By("Save original klusterlet configuration")
		klusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(
			context.TODO(), universalKlusterletName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		if klusterlet.Spec.RegistrationConfiguration != nil {
			originalAddOnDriver = klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver
		}

		ginkgo.By("Get initial registration agent deployment generation before updating klusterlet")
		var initialGeneration int64
		var registrationDeploymentName string
		registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
		if klusterlet.Spec.DeployOption.Mode == operatorapiv1.InstallModeSingleton ||
			klusterlet.Spec.DeployOption.Mode == operatorapiv1.InstallModeSingletonHosted {
			registrationDeploymentName = fmt.Sprintf("%s-agent", klusterlet.Name)
		}

		// In hosted mode, agents run on the hub cluster, otherwise on the spoke cluster
		agentClient = spoke.KubeClient
		agentNamespace = universalAgentNamespace
		if klusterlet.Spec.DeployOption.Mode == operatorapiv1.InstallModeHosted ||
			klusterlet.Spec.DeployOption.Mode == operatorapiv1.InstallModeSingletonHosted {
			agentClient = hub.KubeClient
			agentNamespace = klusterlet.Name
		}

		deployment, err := agentClient.AppsV1().Deployments(agentNamespace).Get(
			context.TODO(), registrationDeploymentName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		initialGeneration = deployment.Generation

		ginkgo.By("Update klusterlet to use token-based authentication for addons")
		gomega.Eventually(func() error {
			klusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(
				context.TODO(), universalKlusterletName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if klusterlet.Spec.RegistrationConfiguration == nil {
				klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{}
			}

			klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver = &operatorapiv1.AddOnRegistrationDriver{
				AuthType: "token",
				Token: &operatorapiv1.TokenConfig{
					ExpirationSeconds: 3600, // 1 hour for testing
				},
			}

			_, err = spoke.OperatorClient.OperatorV1().Klusterlets().Update(
				context.TODO(), klusterlet, metav1.UpdateOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Verify klusterlet is updated with token auth configuration")
		gomega.Eventually(func() error {
			klusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(
				context.TODO(), universalKlusterletName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if klusterlet.Spec.RegistrationConfiguration == nil ||
				klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver == nil {
				return fmt.Errorf("token auth configuration not set")
			}

			if klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver.AuthType != "token" {
				return fmt.Errorf("auth type is not token: %s",
					klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver.AuthType)
			}

			return nil
		}).Should(gomega.Succeed())

		ginkgo.By("Wait for registration agent deployment to rollout with new token auth configuration")
		gomega.Eventually(func() error {
			deployment, err := agentClient.AppsV1().Deployments(agentNamespace).Get(
				context.TODO(), registrationDeploymentName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Wait for deployment generation to increment (indicates config change was applied)
			if deployment.Generation <= initialGeneration {
				return fmt.Errorf("deployment generation has not incremented yet: current=%d, initial=%d",
					deployment.Generation, initialGeneration)
			}

			// Ensure the deployment controller has observed the latest spec
			if deployment.Status.ObservedGeneration != deployment.Generation {
				return fmt.Errorf("deployment has not observed latest generation: observed=%d, current=%d",
					deployment.Status.ObservedGeneration, deployment.Generation)
			}

			// Ensure all replicas have been updated with the new configuration
			if deployment.Status.UpdatedReplicas != deployment.Status.Replicas {
				return fmt.Errorf("deployment has not updated all replicas: updated=%d, total=%d",
					deployment.Status.UpdatedReplicas, deployment.Status.Replicas)
			}

			// Ensure all updated replicas are ready
			if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
				return fmt.Errorf("deployment not fully ready: ready=%d, total=%d",
					deployment.Status.ReadyReplicas, deployment.Status.Replicas)
			}

			// Ensure there are no unavailable replicas
			if deployment.Status.UnavailableReplicas > 0 {
				return fmt.Errorf("deployment has unavailable replicas: %d", deployment.Status.UnavailableReplicas)
			}

			return nil
		}, "2m", "5s").Should(gomega.Succeed())

		signerSecretNamespace = "signer-secret-token-ns-" + rand.String(6)
		ginkgo.By("Create addon custom sign secret namespace")
		_, err = hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: signerSecretNamespace,
			},
		}, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By("Create addon custom sign secret")
		err = copySignerSecret(context.TODO(), hub.KubeClient, "open-cluster-management-hub",
			"signer-secret", signerSecretNamespace, customSignerSecretName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

	})

	ginkgo.AfterAll(func() {
		ginkgo.By("Delete addon custom sign secret")
		err := hub.KubeClient.CoreV1().Secrets(signerSecretNamespace).Delete(context.TODO(),
			customSignerSecretName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.Fail(fmt.Sprintf("failed to delete custom signer secret %v/%v: %v",
				signerSecretNamespace, customSignerSecretName, err))
		}

		ginkgo.By("Delete addon custom sign secret namespace")
		err = hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), signerSecretNamespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.Fail(fmt.Sprintf("failed to delete custom signer secret namespace %v: %v", signerSecretNamespace, err))
		}

		ginkgo.By("Restore original klusterlet AddOnKubeClientRegistrationDriver configuration")
		gomega.Eventually(func() error {
			klusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(
				context.TODO(), universalKlusterletName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if klusterlet.Spec.RegistrationConfiguration == nil {
				klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{}
			}

			klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver = originalAddOnDriver

			_, err = spoke.OperatorClient.OperatorV1().Klusterlets().Update(
				context.TODO(), klusterlet, metav1.UpdateOptions{})
			return err
		}).Should(gomega.Succeed())

	})

	ginkgo.It("Should work with token-based authentication flow", func() {
		var err error

		ginkgo.By("Step 1: Create addon template resources")
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

		ginkgo.By("Step 2: Create the template addon")
		err = hub.CreateManagedClusterAddOn(universalClusterName, addOnName, addonInstallNamespace)
		if err != nil {
			gomega.Expect(errors.IsAlreadyExists(err)).To(gomega.BeTrue())
		}

		ginkgo.By("Step 3: Wait for addon to become available with token authentication")
		gomega.Eventually(func() error {
			return hub.CheckManagedClusterAddOnStatus(universalClusterName, addOnName)
		}, "5m", "10s").Should(gomega.Succeed())

		ginkgo.By("Step 4: Verify hub kubeconfig secret is created with token authentication")
		gomega.Eventually(func() error {
			secret, err := agentClient.CoreV1().Secrets(addonInstallNamespace).Get(context.TODO(),
				templateagent.HubKubeconfigSecretName(addOnName), metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Verify the secret contains token-based kubeconfig
			token, ok := secret.Data["token"]
			if !ok {
				return fmt.Errorf("token not found in secret")
			}

			// Token should not be empty
			if len(token) == 0 {
				return fmt.Errorf("token is empty")
			}

			return nil
		}).Should(gomega.Succeed())

		ginkgo.By("Step 5: Verify custom client cert secret is created")
		gomega.Eventually(func() error {
			_, err := agentClient.CoreV1().Secrets(addonInstallNamespace).Get(context.TODO(),
				templateagent.CustomSignedSecretName(addOnName, customSignerName), metav1.GetOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Step 6: Test addon functionality - create configmap on hub")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("config-token-%s", rand.String(6)),
				Namespace: universalClusterName,
			},
			Data: map[string]string{
				"key1": rand.String(6),
				"key2": rand.String(6),
			},
		}

		_, err = hub.KubeClient.CoreV1().ConfigMaps(universalClusterName).Create(
			context.Background(), configmap, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Step 7: Verify addon copies configmap to spoke using token auth")
		gomega.Eventually(func() error {
			copiedConfig, err := spoke.KubeClient.CoreV1().ConfigMaps(addonInstallNamespace).Get(
				context.Background(), configmap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !equality.Semantic.DeepEqual(copiedConfig.Data, configmap.Data) {
				return fmt.Errorf("expected configmap is not correct, %v", copiedConfig.Data)
			}
			return nil
		}, "2m", "5s").ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Step 8: Cleanup - Delete the addon")
		err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Delete(
			context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err == nil {
				return fmt.Errorf("the managedClusterAddon %s should be deleted", addOnName)
			}
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
			return nil
		}).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Step 9: Delete addon template resources")
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

		ginkgo.By("Step 10: Cleanup CSRs")
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
})
