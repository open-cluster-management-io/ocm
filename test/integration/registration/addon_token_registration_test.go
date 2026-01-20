package registration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/addon"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Addon Token Registration", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir, addOnName string
	var err error
	var cancel context.CancelFunc
	var cancelAddonManager context.CancelFunc
	var bootstrapKubeconfig string
	var expectedProxyURL string
	var signerName = certificates.KubeAPIServerClientSignerName

	ginkgo.BeforeEach(func() {
		// Start the addon manager which includes the tokenInfrastructureController
		// This controller creates ServiceAccounts for token-based authentication
		addonMgrCtx, addonMgrCancel := context.WithCancel(context.Background())
		cancelAddonManager = addonMgrCancel

		go func() {
			defer ginkgo.GinkgoRecover()
			err := addon.RunManager(addonMgrCtx, &controllercmd.ControllerContext{
				KubeConfig:    hubCfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("addon-manager"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
	})

	ginkgo.JustBeforeEach(func() {
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

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		if cancelAddonManager != nil {
			cancelAddonManager()
		}
	})

	assertSuccessClusterBootstrap := func() {
		// the spoke cluster and csr should be created after bootstrap
		ginkgo.By("Check existence of ManagedCluster & CSR")
		gomega.Eventually(func() error {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// the spoke cluster should has finalizer that is added by hub controller
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}

			if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Accept and approve the ManagedCluster")
		// simulate hub cluster admin to accept the managedcluster and approve the csr
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the managed cluster should have accepted condition after it is accepted
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			accepted := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
			return accepted != nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("ManagedCluster joins the hub")
		// the spoke cluster should have joined condition finally
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			joined := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
			return joined != nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// ensure cluster namespace is in place
		gomega.Eventually(func() bool {
			_, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
			return err == nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertValidTokenCredential := func(secretNamespace, secretName, expectedProxyURL string) {
		ginkgo.By("Check token credential in secret")
		gomega.Eventually(func() bool {
			secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertAddonLabel := func(clusterName, addonName, status string) {
		ginkgo.By("Check addon status label on managed cluster")
		gomega.Eventually(func() bool {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(cluster.Labels) == 0 {
				return false
			}
			key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addonName)
			return cluster.Labels[key] == status
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertTokenRefreshedCondition := func(clusterName, addonName string) {
		ginkgo.By("Check token refreshed addon status condition")
		gomega.Eventually(func() bool {
			addon, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).
				Get(context.TODO(), addonName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(addon.Status.Conditions, token.TokenRefreshedCondition)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertSuccessCSRApproval := func() {
		ginkgo.By("Approve bootstrap csr")
		var csr *certificates.CertificateSigningRequest
		gomega.Eventually(func() bool {
			csr, err = util.FindUnapprovedAddOnCSR(kubeClient, managedClusterName, addOnName)
			return err == nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		now := time.Now()
		err = authn.ApproveCSR(kubeClient, csr, now.UTC(), now.Add(30*time.Second).UTC())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	assertValidClientCertificate := func(secretNamespace, secretName, expectedProxyURL string) {
		ginkgo.By("Check client certificate in secret")
		gomega.Eventually(func() bool {
			secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if _, ok := secret.Data[csr.TLSKeyFile]; !ok {
				return false
			}
			if _, ok := secret.Data[csr.TLSCertFile]; !ok {
				return false
			}
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
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertHasNoAddonLabel := func(clusterName, addonName string) {
		ginkgo.By("Check if addon status label on managed cluster deleted")
		gomega.Eventually(func() bool {
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
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertAddOnSignerUpdate := func(signerName string) {
		gomega.Eventually(func() error {
			addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addOn.Status.Registrations = []addonv1alpha1.RegistrationConfig{
				{
					SignerName: signerName,
				},
			}
			_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	}

	assertSuccessAddOnEnabling := func() {
		ginkgo.By("Create ManagedClusterAddOn cr for token authentication")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// create addon
		addOn := &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		}
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	assertSuccessAddOnBootstrap := func(signerName string) {
		assertSuccessAddOnEnabling()
		assertAddOnSignerUpdate(signerName)
		assertValidTokenCredential(addOnName, getSecretName(addOnName, signerName), expectedProxyURL)
		assertAddonLabel(managedClusterName, addOnName, "unreachable")
		assertTokenRefreshedCondition(managedClusterName, addOnName)
	}

	assertSecretGone := func(secretNamespace, secretName string) {
		gomega.Eventually(func() bool {
			_, err = kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertRegistrationSucceed := func() {
		ginkgo.It("should register addon with token successfully", func() {
			assertSuccessClusterBootstrap()
			assertSuccessAddOnBootstrap(signerName)

			ginkgo.By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			assertSecretGone(addOnName, getSecretName(addOnName, signerName))

			assertHasNoAddonLabel(managedClusterName, addOnName)
		})
	}

	ginkgo.Context("without proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
			expectedProxyURL = ""
		})
		assertRegistrationSucceed()

		ginkgo.It("should register addon with token successfully even when the install namespace is not available at the beginning", func() {
			assertSuccessClusterBootstrap()

			ginkgo.By("Create ManagedClusterAddOn cr for token authentication")

			// create addon
			addOn := &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOnName,
					Namespace: managedClusterName,
				},
				Spec: addonv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: addOnName,
				},
			}
			_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				created, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				created.Status.Registrations = []addonv1alpha1.RegistrationConfig{
					{
						SignerName: signerName,
					},
				}
				_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Wait for addon namespace - token should not be created yet")
			gomega.Consistently(func() bool {
				_, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), getSecretName(addOnName, signerName), metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, 10*time.Second, 2*time.Second).Should(gomega.BeTrue())

			ginkgo.By("Create addon namespace")
			// create addon namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: addOnName,
				},
			}
			_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			assertValidTokenCredential(addOnName, getSecretName(addOnName, signerName), expectedProxyURL)

			ginkgo.By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			assertSecretGone(addOnName, getSecretName(addOnName, signerName))
		})

		ginkgo.It("should rotate token successfully", func() {
			assertSuccessClusterBootstrap()
			assertSuccessAddOnBootstrap(signerName)

			secretName := getSecretName(addOnName, signerName)
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			originalToken := secret.Data[token.TokenFile]
			gomega.Expect(originalToken).NotTo(gomega.BeNil())

			ginkgo.By("Trigger token rotation by deleting the ServiceAccount")
			// ServiceAccount naming convention: <addon-name>-agent
			serviceAccountName := fmt.Sprintf("%s-agent", addOnName)
			err = kubeClient.CoreV1().ServiceAccounts(managedClusterName).Delete(context.TODO(), serviceAccountName, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Wait for ServiceAccount to be recreated")
			gomega.Eventually(func() bool {
				_, err := kubeClient.CoreV1().ServiceAccounts(managedClusterName).Get(context.TODO(), serviceAccountName, metav1.GetOptions{})
				return err == nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Wait for token rotation")
			gomega.Eventually(func() bool {
				newSecret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				newToken := newSecret.Data[token.TokenFile]
				if newToken == nil {
					return false
				}

				// Verify the token has changed
				return !reflect.DeepEqual(originalToken, newToken)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("should switch from token to CSR driver successfully", func() {
			assertSuccessClusterBootstrap()
			assertSuccessAddOnBootstrap(signerName)

			secretName := getSecretName(addOnName, signerName)

			ginkgo.By("Verify token-based secret exists")
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(secret.Data[token.TokenFile]).NotTo(gomega.BeNil())

			ginkgo.By("Stop the agent and restart with CSR driver")
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

			ginkgo.By("Update addon registration with subject for CSR")
			gomega.Eventually(func() error {
				addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
					Get(context.TODO(), addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				addOn.Status.Registrations = []addonv1alpha1.RegistrationConfig{
					{
						SignerName: signerName,
					},
				}
				_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
					UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Approve CSR and verify client certificate")
			assertSuccessCSRApproval()
			assertValidClientCertificate(addOnName, secretName, expectedProxyURL)

			ginkgo.By("Verify CSR credentials exist in secret")
			gomega.Eventually(func() bool {
				secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, hasCert := secret.Data[csr.TLSCertFile]
				_, hasKey := secret.Data[csr.TLSKeyFile]
				// Cert and key should exist (token may remain as leftover)
				return hasCert && hasKey
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("should switch from CSR to token driver successfully", func() {
			ginkgo.By("Start with CSR driver")
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

			assertSuccessClusterBootstrap()

			ginkgo.By("Create addon with CSR-based registration")
			assertSuccessAddOnEnabling()

			gomega.Eventually(func() error {
				addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
					Get(context.TODO(), addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				addOn.Status.Registrations = []addonv1alpha1.RegistrationConfig{
					{
						SignerName: signerName,
					},
				}
				_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
					UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			secretName := getSecretName(addOnName, signerName)

			ginkgo.By("Approve CSR and verify client certificate")
			assertSuccessCSRApproval()
			assertValidClientCertificate(addOnName, secretName, expectedProxyURL)

			ginkgo.By("Verify CSR-based secret exists")
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(secret.Data[csr.TLSCertFile]).NotTo(gomega.BeNil())
			gomega.Expect(secret.Data[csr.TLSKeyFile]).NotTo(gomega.BeNil())

			ginkgo.By("Stop the agent and restart with token driver")
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

			ginkgo.By("Update addon registration to remove subject (for token)")
			gomega.Eventually(func() error {
				addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
					Get(context.TODO(), addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				addOn.Status.Registrations = []addonv1alpha1.RegistrationConfig{
					{
						SignerName: signerName,
						// No subject - will be set by token driver
					},
				}
				_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
					UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Verify token credential is created")
			assertValidTokenCredential(addOnName, secretName, expectedProxyURL)
			assertTokenRefreshedCondition(managedClusterName, addOnName)

			ginkgo.By("Verify token credentials exist in secret")
			gomega.Eventually(func() bool {
				secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, hasToken := secret.Data[token.TokenFile]
				// Token should exist (cert and key may remain as leftover)
				return hasToken
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("with http proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPProxyFile
			expectedProxyURL = httpProxyURL
		})
		assertRegistrationSucceed()
	})

	ginkgo.Context("with https proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPSProxyFile
			expectedProxyURL = httpsProxyURL
		})
		assertRegistrationSucceed()
	})
})
