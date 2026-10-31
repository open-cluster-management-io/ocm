package registration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Addon Alpha Registration", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir, addOnName string
	var err error
	var cancel context.CancelFunc
	var bootstrapKubeconfig string
	var expectedProxyURL string

	JustBeforeEach(func(ctx SpecContext) {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("addontest-%s", suffix), "hub-kubeconfig")
		addOnName = fmt.Sprintf("addon-%s", suffix)

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeconfig,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}

		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel = runAgent("addontest", agentOptions, commOptions, spokeCfg)
	})

	AfterEach(
		func() {
			if cancel != nil {
				cancel()
			}
		})

	assertSuccessCSRApproval := func() {
		By("Approve bootstrap csr")
		var csr *certificates.CertificateSigningRequest
		Eventually(func() error {
			csr, err = util.FindUnapprovedAddOnCSR(kubeClient, managedClusterName, addOnName)
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		now := time.Now()
		err = authn.ApproveCSR(kubeClient, csr, now.UTC(), now.Add(30*time.Second).UTC())
		Expect(err).NotTo(HaveOccurred())
	}

	assertValidClientCertificate := func(ctx context.Context, secretNamespace, secretName, signerName, expectedProxyURL string) {
		By("Check client certificate in secret")
		Eventually(func() bool {
			secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
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

			if signerName == certificates.KubeAPIServerClientSignerName {
				if !ok {
					return false
				}

				proxyURL, err := getProxyURLFromKubeconfigData(kubeconfigData)
				if err != nil {
					return false
				}

				return proxyURL == expectedProxyURL
			}

			if ok && signerName != certificates.KubeAPIServerClientSignerName {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertClientCertCondition := func(ctx context.Context, clusterName, addonName string) {
		By("Check clientcert addon status condition")
		Eventually(func() bool {
			addon, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).
				Get(ctx, addonName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(addon.Status.Conditions, csr.ClusterCertificateRotatedCondition)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertHasNoAddonLabel := func(clusterName, addonName string) {
		By("Check if addon status label on managed cluster deleted")
		Eventually(func() bool {
			cluster, err := util.GetManagedCluster(clusterClient, clusterName)
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

	assertAddOnSignerUpdate := func(ctx context.Context, signerName string) {
		Eventually(func() error {
			addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				Get(ctx, addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			addOn.Status.Registrations = []addonv1alpha1.RegistrationConfig{
				{
					SignerName: signerName,
				},
			}
			_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).
				UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	}

	assertSuccessAddOnEnabling := func(ctx context.Context) {
		By("Create ManagedClusterAddOn cr with required annotations")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

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
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		_, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	assertSuccessAddOnBootstrap := func(ctx context.Context, signerName string) {
		assertSuccessAddOnEnabling(ctx)
		assertAddOnSignerUpdate(ctx, signerName)
		assertSuccessCSRApproval()
		assertValidClientCertificate(ctx, addOnName, getAlphaSecretName(addOnName, signerName), signerName, expectedProxyURL)
		assertAddonLabel(managedClusterName, addOnName, "unreachable")
		assertClientCertCondition(ctx, managedClusterName, addOnName)
	}

	assertSecretGone := func(ctx context.Context, secretNamespace, secretName string) {
		Eventually(func() bool {
			_, err = kubeClient.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertRegistrationSucceed := func() {
		It("should register addon successfully", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			signerName := certificates.KubeAPIServerClientSignerName
			assertSuccessAddOnBootstrap(ctx, signerName)

			By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(ctx, addOnName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			assertSecretGone(ctx, addOnName, getAlphaSecretName(addOnName, signerName))

			assertHasNoAddonLabel(managedClusterName, addOnName)
		})
	}

	Context("without proxy", func() {
		BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
			expectedProxyURL = ""
		})
		assertRegistrationSucceed()

		It("should register addon successfully even when the install namespace is not available at the beginning", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)

			signerName := certificates.KubeAPIServerClientSignerName
			By("Create ManagedClusterAddOn cr with required annotations")

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
			_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(ctx, addOn, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				created, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				created.Status.Registrations = []addonv1alpha1.RegistrationConfig{
					{
						SignerName: signerName,
					},
				}
				_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, created, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			assertSuccessCSRApproval()

			By("Wait for addon namespace")
			Consistently(func() error {
				csrs, err := util.FindAddOnCSRs(kubeClient, managedClusterName, addOnName)
				if err != nil {
					return err
				}

				if len(csrs) != 1 {
					return fmt.Errorf("the number of CSRs is not correct, got %d", len(csrs))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Create addon namespace")
			// create addon namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: addOnName,
				},
			}
			_, err = kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			assertValidClientCertificate(ctx, addOnName, getAlphaSecretName(addOnName, signerName), signerName, expectedProxyURL)

			By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(ctx, addOnName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			assertSecretGone(ctx, addOnName, getAlphaSecretName(addOnName, signerName))
		})

		It("should register addon with custom signer successfully", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			signerName := "example.com/signer1"
			assertSuccessAddOnBootstrap(ctx, signerName)
		})

		It("should addon registration config updated successfully", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			signerName := certificates.KubeAPIServerClientSignerName
			assertSuccessAddOnBootstrap(ctx, signerName)

			// update registration config and change the signer
			newSignerName := "example.com/signer1"
			Eventually(func() error {
				addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				addOn.Status.Registrations = []addonv1alpha1.RegistrationConfig{
					{
						SignerName: newSignerName,
					},
				}
				_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			assertSecretGone(ctx, addOnName, getAlphaSecretName(addOnName, signerName))

			assertSuccessCSRApproval()
			assertValidClientCertificate(ctx, addOnName, getAlphaSecretName(addOnName, newSignerName), newSignerName, expectedProxyURL)
		})

		It("should rotate addon client cert successfully", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			signerName := certificates.KubeAPIServerClientSignerName
			assertSuccessAddOnBootstrap(ctx, signerName)

			secretName := getAlphaSecretName(addOnName, signerName)
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			By("Wait for cert rotation")
			assertSuccessCSRApproval()
			Eventually(func() bool {
				newSecret, err := kubeClient.CoreV1().Secrets(addOnName).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				return !reflect.DeepEqual(secret.Data, newSecret.Data)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should stop addon client cert update if too frequent", func(ctx SpecContext) {
			assertSuccessClusterBootstrap(ctx, managedClusterName, hubKubeconfigSecret)
			signerName := certificates.KubeAPIServerClientSignerName
			assertSuccessAddOnBootstrap(ctx, signerName)

			// update subject for 15 times
			for i := 1; i <= 15; i++ {
				currentIndex := i
				Eventually(func() error {
					addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					if len(addOn.Status.Registrations) == 0 {
						return fmt.Errorf("no registrations found")
					}
					addOn.Status.Registrations = []addonv1alpha1.RegistrationConfig{
						{
							//nolint:staticcheck // SA1019: v1alpha1 registration still uses signerName.
							SignerName: addOn.Status.Registrations[0].SignerName,
							Subject: addonv1alpha1.Subject{
								User: fmt.Sprintf("test-%d", currentIndex),
							},
						},
					}
					_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(ctx, addOn, metav1.UpdateOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
				// sleep 1 second to ensure controller issue a new csr.
				time.Sleep(1 * time.Second)
			}

			By("CSR should not exceed 10")
			csrs, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s,%s=%s", clusterv1.ClusterNameLabelKey, managedClusterName, addonv1alpha1.AddonLabelKey, addOnName),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(csrs.Items)).To(BeNumerically("<=", 10))

			Eventually(func() error {
				addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(ctx, addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if meta.IsStatusConditionFalse(addOn.Status.Conditions, "ClusterCertificateRotated") {
					return nil
				}

				return fmt.Errorf("addon status is not correct, got %v", addOn.Status.Conditions)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		})
	})

	Context("with http proxy", func() {
		BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPProxyFile
			expectedProxyURL = httpProxyURL
		})
		assertRegistrationSucceed()
	})

	Context("with https proxy", func() {
		BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPSProxyFile
			expectedProxyURL = httpsProxyURL
		})
		assertRegistrationSucceed()
	})
})

func getAlphaSecretName(addOnName, signerName string) string {
	if signerName == certificates.KubeAPIServerClientSignerName {
		return fmt.Sprintf("%s-hub-kubeconfig", addOnName)
	}
	return fmt.Sprintf("%s-%s-client-cert", addOnName, strings.ReplaceAll(signerName, "/", "-"))
}
