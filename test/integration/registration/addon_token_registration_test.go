package registration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	"open-cluster-management.io/ocm/pkg/addon"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Addon Token Registration", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir, addOnName string
	var err error
	var cancel context.CancelFunc
	var cancelAddonManager context.CancelFunc
	var bootstrapKubeconfig string
	var expectedProxyURL string

	BeforeEach(func() {
		// Start the addon manager which includes the tokenInfrastructureController
		// This controller creates ServiceAccounts for token-based authentication
		addonMgrCtx, addonMgrCancel := context.WithCancel(context.Background())
		cancelAddonManager = addonMgrCancel

		go func() {
			defer GinkgoRecover()
			err := addon.RunManager(addonMgrCtx, &controllercmd.ControllerContext{
				KubeConfig:    hubCfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("addon-manager"),
			})
			Expect(err).NotTo(HaveOccurred())
		}()
	})

	JustBeforeEach(func(ctx SpecContext) {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("token-cluster-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("token-hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("token-addontest-%s", suffix), "hub-kubeconfig")
		addOnName = fmt.Sprintf("token-addon-%s", suffix)

		// Create agent options with token authentication for addons
		driverOption := registerfactory.NewOptions()
		driverOption.AddonKubeClientRegistrationAuth = "token" // Use token-based authentication for addons

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeconfig,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     driverOption,
		}

		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel = runAgent("token-addontest", agentOptions, commOptions, spokeCfg)
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		if cancelAddonManager != nil {
			cancelAddonManager()
		}
	})

	assertValidTokenCredential := func(ctx context.Context, secretNamespace, secretName, expectedProxyURL string) {
		By("Check token credential in secret")
		Eventually(func() bool {
			secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			// Verify token exists
			if _, ok := secret.Data[token.TokenFile]; !ok {
				return false
			}
			// Verify kubeconfig with token
			kubeconfigData, ok := secret.Data[register.KubeconfigFile]
			if !ok {
				return false
			}

			if expectedProxyURL != "" {
				proxyURL, err := getProxyURLFromKubeconfigData(kubeconfigData)
				if err != nil {
					return false
				}
				return proxyURL == expectedProxyURL
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertTokenRefreshedCondition := func(ctx context.Context, clusterName, addonName string) {
		By("Check token refreshed addon status condition")
		Eventually(func() bool {
			addon, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(clusterName).
				Get(ctx, addonName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(addon.Status.Conditions, token.TokenRefreshedCondition)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertSuccessCSRApproval := func() {
		By("Approve bootstrap csr")
		var csr *certificates.CertificateSigningRequest
		Eventually(func() bool {
			csr, err = util.FindUnapprovedAddOnCSR(kubeClient, managedClusterName, addOnName)
			return err == nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		now := time.Now()
		err = authn.ApproveCSR(kubeClient, csr, now.UTC(), now.Add(30*time.Second).UTC())
		Expect(err).NotTo(HaveOccurred())
	}

	assertValidClientCertificate := func(ctx context.Context, secretNamespace, secretName, expectedProxyURL string) {
		By("Check client certificate in secret")
		Eventually(func() error {
			secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if _, ok := secret.Data[csr.TLSKeyFile]; !ok {
				return fmt.Errorf("key of client certificate not found in secret")
			}
			if _, ok := secret.Data[csr.TLSCertFile]; !ok {
				return fmt.Errorf("certificate of client certificate not found in secret")
			}
			kubeconfigData, ok := secret.Data[register.KubeconfigFile]
			if !ok {
				return fmt.Errorf("kubeconfig of client certificate not found in secret")
			}

			if expectedProxyURL != "" {
				proxyURL, err := getProxyURLFromKubeconfigData(kubeconfigData)
				if err != nil {
					return fmt.Errorf("invalid proxy URL found in secret")
				}
				if proxyURL != expectedProxyURL {
					return fmt.Errorf("invalid proxy URL found in secret")
				}
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	}

	assertHasNoAddonLabel := func(clusterName, addonName string) {
		By("Check if addon status label on managed cluster deleted")
		Eventually(func() bool {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(cluster.Labels) == 0 {
				return true
			}
			key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addonName)
			_, ok := cluster.Labels[key]
			return !ok
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertAddOnRegistrationUpdate := func(ctx context.Context, namespace string) {
		Eventually(func() error {
			addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
				Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addOn.Status.Registrations = []addonv1beta1.RegistrationConfig{
				{
					Type: addonv1beta1.KubeClient,
				},
			}
			addOn.Status.Namespace = namespace
			_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
				UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	}

	assertSuccessAddOnEnabling := func(ctx context.Context) {
		By("Create ManagedClusterAddOn cr for token authentication")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// create addon
		addOn := &addonv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		_, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	assertSuccessAddOnBootstrap := func(ctx context.Context, registrationType addonv1beta1.RegistrationType, signerName string) {
		assertSuccessAddOnEnabling(ctx)
		assertAddOnRegistrationUpdate(ctx, addOnName)
		assertValidTokenCredential(ctx, addOnName, getSecretName(registrationType, addOnName, signerName), expectedProxyURL)
		assertAddonLabel(managedClusterName, addOnName, "unreachable")
		assertTokenRefreshedCondition(ctx, managedClusterName, addOnName)
	}

	assertSecretGone := func(ctx context.Context, secretNamespace, secretName string) {
		Eventually(func() bool {
			_, err = kubeClient.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertRegistrationSucceed := func(registrationType addonv1beta1.RegistrationType, signerName string) {
		It("should register addon with token successfully", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			assertSuccessAddOnBootstrap(ctx, registrationType, signerName)

			By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Delete(ctx, addOnName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			assertSecretGone(ctx, addOnName, getSecretName(registrationType, addOnName, signerName))

			assertHasNoAddonLabel(managedClusterName, addOnName)
		})
	}

	Context("without proxy", func() {
		BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
			expectedProxyURL = ""
		})
		assertRegistrationSucceed(addonv1beta1.KubeClient, "")

		It("should register addon with token successfully even when the install namespace is not available at the beginning", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)

			By("Create ManagedClusterAddOn cr for token authentication")

			// create addon
			addOn := &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOnName,
					Namespace: managedClusterName,
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
			}
			_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				created, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				created.Status.Registrations = []addonv1beta1.RegistrationConfig{
					{
						Type: addonv1beta1.KubeClient,
					},
				}
				created.Status.Namespace = addOnName
				_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, created, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Wait for addon namespace - token should not be created yet")
			Consistently(func() bool {
				_, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, getSecretName(addonv1beta1.KubeClient, addOnName, ""), metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, 10*time.Second, 2*time.Second).Should(BeTrue())

			By("Create addon namespace")
			// create addon namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: addOnName,
				},
			}
			_, err = kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			assertValidTokenCredential(ctx, addOnName, getSecretName(addonv1beta1.KubeClient, addOnName, ""), expectedProxyURL)

			By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Delete(ctx, addOnName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			assertSecretGone(ctx, addOnName, getSecretName(addonv1beta1.KubeClient, addOnName, ""))
		})

		It("should rotate token successfully", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			assertSuccessAddOnBootstrap(ctx, addonv1beta1.KubeClient, "")

			secretName := getSecretName(addonv1beta1.KubeClient, addOnName, "")
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			originalToken := secret.Data[token.TokenFile]
			Expect(originalToken).NotTo(BeNil())

			By("Trigger token rotation by deleting the ServiceAccount")
			// ServiceAccount naming convention: <addon-name>-agent
			serviceAccountName := fmt.Sprintf("%s-agent", addOnName)
			err = kubeClient.CoreV1().ServiceAccounts(managedClusterName).Delete(ctx, serviceAccountName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Wait for ServiceAccount to be recreated")
			Eventually(func() bool {
				_, err := kubeClient.CoreV1().ServiceAccounts(managedClusterName).Get(ctx, serviceAccountName, metav1.GetOptions{})
				return err == nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Wait for token rotation")
			Eventually(func() bool {
				newSecret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				newToken := newSecret.Data[token.TokenFile]
				if newToken == nil {
					return false
				}

				// Verify the token has changed
				return !reflect.DeepEqual(originalToken, newToken)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should switch from token to CSR driver successfully", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			assertSuccessAddOnBootstrap(ctx, addonv1beta1.KubeClient, "")

			secretName := getSecretName(addonv1beta1.KubeClient, addOnName, "")

			By("Verify token-based secret exists")
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Data[token.TokenFile]).NotTo(BeNil())

			By("Stop the agent and restart with CSR driver")
			cancel()

			// Restart agent with CSR driver
			driverOption := registerfactory.NewOptions()
			driverOption.AddonKubeClientRegistrationAuth = "csr" // Switch to CSR-based authentication

			agentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeconfig,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     driverOption,
			}

			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeconfigDir
			commOptions.SpokeClusterName = managedClusterName

			cancel = runAgent("token-to-csr-test", agentOptions, commOptions, spokeCfg)

			By("Update addon registration with subject for CSR")
			Eventually(func() error {
				addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
					Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				addOn.Status.Registrations = []addonv1beta1.RegistrationConfig{
					{
						Type: addonv1beta1.KubeClient,
					},
				}
				_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
					UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Approve CSR and verify client certificate")
			assertSuccessCSRApproval()
			assertValidClientCertificate(ctx, addOnName, secretName, expectedProxyURL)

			By("Verify CSR credentials exist in secret")
			Eventually(func() bool {
				secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, hasCert := secret.Data[csr.TLSCertFile]
				_, hasKey := secret.Data[csr.TLSKeyFile]
				// Cert and key should exist (token may remain as leftover)
				return hasCert && hasKey
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should switch from CSR to token driver successfully", func(ctx SpecContext) {
			By("Start with CSR driver")
			cancel()

			// Start agent with CSR driver
			driverOption := registerfactory.NewOptions()
			driverOption.AddonKubeClientRegistrationAuth = "csr"

			agentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeconfig,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     driverOption,
			}

			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeconfigDir
			commOptions.SpokeClusterName = managedClusterName

			cancel = runAgent("csr-to-token-test", agentOptions, commOptions, spokeCfg)

			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)

			By("Create addon with CSR-based registration")
			assertSuccessAddOnEnabling(ctx)

			Eventually(func() error {
				addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
					Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				addOn.Status.Registrations = []addonv1beta1.RegistrationConfig{
					{
						Type: addonv1beta1.KubeClient,
					},
				}
				addOn.Status.Namespace = addOnName
				_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
					UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			secretName := getSecretName(addonv1beta1.KubeClient, addOnName, "")

			By("Approve CSR and verify client certificate")
			assertSuccessCSRApproval()
			assertValidClientCertificate(ctx, addOnName, secretName, expectedProxyURL)

			By("Verify CSR-based secret exists")
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Data[csr.TLSCertFile]).NotTo(BeNil())
			Expect(secret.Data[csr.TLSKeyFile]).NotTo(BeNil())

			By("Stop the agent and restart with token driver")
			cancel()

			// Restart agent with token driver
			driverOption = registerfactory.NewOptions()
			driverOption.AddonKubeClientRegistrationAuth = "token"

			agentOptions = &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeconfig,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     driverOption,
			}

			commOptions = commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeconfigDir
			commOptions.SpokeClusterName = managedClusterName

			cancel = runAgent("csr-to-token-test", agentOptions, commOptions, spokeCfg)

			By("Update addon registration to remove subject (for token)")
			Eventually(func() error {
				addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
					Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				addOn.Status.Registrations = []addonv1beta1.RegistrationConfig{
					{
						Type: addonv1beta1.KubeClient,
					},
				}
				_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
					UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Verify token credential is created")
			assertValidTokenCredential(ctx, addOnName, secretName, expectedProxyURL)
			assertTokenRefreshedCondition(ctx, managedClusterName, addOnName)

			By("Verify token credentials exist in secret")
			Eventually(func() bool {
				secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, hasToken := secret.Data[token.TokenFile]
				// Token should exist (cert and key may remain as leftover)
				return hasToken
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("with http proxy", func() {
		BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPProxyFile
			expectedProxyURL = httpProxyURL
		})
		assertRegistrationSucceed(addonv1beta1.KubeClient, "")
	})

	Context("with https proxy", func() {
		BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPSProxyFile
			expectedProxyURL = httpsProxyURL
		})
		assertRegistrationSucceed(addonv1beta1.KubeClient, "")
	})
})
