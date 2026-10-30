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

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Addon Registration", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir, addOnName string
	var err error
	var cancel context.CancelFunc
	var bootstrapKubeconfig string
	var expectedProxyURL string

	JustBeforeEach(func() {
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
			cancel()
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

	assertValidClientCertificate := func(registrationType addonv1beta1.RegistrationType, secretNamespace, secretName, expectedProxyURL string) {
		By("Check client certificate in secret")
		Eventually(func() error {
			secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if _, ok := secret.Data[csr.TLSKeyFile]; !ok {
				return fmt.Errorf("key of client certificate not found in secret")
			}
			if _, ok := secret.Data[csr.TLSCertFile]; !ok {
				return fmt.Errorf("cert of client certificate not found in secret")
			}
			kubeconfigData, ok := secret.Data[register.KubeconfigFile]

			if registrationType == addonv1beta1.KubeClient {
				if !ok {
					return fmt.Errorf("kubeconfig not found in secret")
				}

				proxyURL, err := getProxyURLFromKubeconfigData(kubeconfigData)
				if err != nil {
					return fmt.Errorf("failed to get proxy URL from kubeconfig")
				}

				if proxyURL != expectedProxyURL {
					return fmt.Errorf("expected proxy URL %s, got %s", expectedProxyURL, proxyURL)
				}
				return nil
			}

			if ok && registrationType != addonv1beta1.KubeClient {
				return fmt.Errorf("expected kubeconfig not found in secret when type is not kubeclient")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	}

	assertClientCertCondition := func(clusterName, addonName string) {
		By("Check clientcert addon status condition")
		Eventually(func() bool {
			addon, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(clusterName).
				Get(context.TODO(), addonName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(addon.Status.Conditions, csr.ClusterCertificateRotatedCondition)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
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

	assertAddOnRegistrationUpdate := func(registrationType addonv1beta1.RegistrationType, signer string) {
		By("Update addon registrations and install namespace")
		Eventually(func() error {
			addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
				Get(context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			var registration addonv1beta1.RegistrationConfig
			switch registrationType {
			case addonv1beta1.KubeClient:
				registration = addonv1beta1.RegistrationConfig{
					Type: registrationType,
				}
			case addonv1beta1.CustomSigner:
				registration = addonv1beta1.RegistrationConfig{
					Type: registrationType,
					CustomSigner: &addonv1beta1.CustomSignerConfig{
						SignerName: signer,
					},
				}
			}
			addOn.Status.Registrations = []addonv1beta1.RegistrationConfig{
				registration,
			}
			addOn.Status.Namespace = addOnName
			_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).
				UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	}

	assertSuccessAddOnEnabling := func() {
		By("Create ManagedClusterAddOn cr with required annotations")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// create addon
		addOn := &addonv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	assertSuccessAddOnBootstrap := func(registrationType addonv1beta1.RegistrationType, signerName string) {
		assertSuccessAddOnEnabling()
		assertAddOnRegistrationUpdate(registrationType, signerName)
		assertSuccessCSRApproval()
		assertValidClientCertificate(registrationType, addOnName, getSecretName(registrationType, addOnName, signerName), expectedProxyURL)
		assertAddonLabel(managedClusterName, addOnName, "unreachable")
		assertClientCertCondition(managedClusterName, addOnName)
	}

	assertSecretGone := func(secretNamespace, secretName string) {
		Eventually(func() bool {
			_, err = kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertRegistrationSucceed := func() {
		It("should register addon successfully", func() {
			assertSuccessClusterBootstrap(managedClusterName, hubKubeconfigSecret)
			assertSuccessAddOnBootstrap(addonv1beta1.KubeClient, "")

			By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			assertSecretGone(addOnName, getSecretName(addonv1beta1.KubeClient, addOnName, ""))

			assertHasNoAddonLabel(managedClusterName, addOnName)
		})
	}

	Context("without proxy", func() {
		BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
			expectedProxyURL = ""
		})
		assertRegistrationSucceed()

		It("should register addon successfully even when the install namespace is not available at the beginning", func() {
			assertSuccessClusterBootstrap(managedClusterName, hubKubeconfigSecret)

			By("Create ManagedClusterAddOn cr with required annotations")

			// create addon
			addOn := &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      addOnName,
					Namespace: managedClusterName,
				},
				Spec: addonv1beta1.ManagedClusterAddOnSpec{},
			}
			_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				created, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				created.Status.Registrations = []addonv1beta1.RegistrationConfig{
					{
						Type: addonv1beta1.KubeClient,
					},
				}
				created.Status.Namespace = addOnName
				_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
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
			_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			assertValidClientCertificate(addonv1beta1.KubeClient, addOnName, getSecretName(addonv1beta1.KubeClient, addOnName, ""), expectedProxyURL)

			By("Delete the addon and check if secret is gone")
			err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			assertSecretGone(addOnName, getSecretName(addonv1beta1.KubeClient, addOnName, ""))
		})

		It("should register addon with custom signer successfully", func() {
			assertSuccessClusterBootstrap(managedClusterName, hubKubeconfigSecret)
			signerName := "example.com/signer1"
			assertSuccessAddOnBootstrap(addonv1beta1.CustomSigner, signerName)
		})

		It("should addon registration config updated successfully", func() {
			assertSuccessClusterBootstrap(managedClusterName, hubKubeconfigSecret)
			assertSuccessAddOnBootstrap(addonv1beta1.KubeClient, "")

			// update registration config and change the signer
			newSignerName := "example.com/signer1"
			Eventually(func() error {
				addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				addOn.Status.Registrations = []addonv1beta1.RegistrationConfig{
					{
						Type: addonv1beta1.CustomSigner,
						CustomSigner: &addonv1beta1.CustomSignerConfig{
							SignerName: newSignerName,
						},
					},
				}
				_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			assertSecretGone(addOnName, getSecretName(addonv1beta1.KubeClient, addOnName, ""))

			assertSuccessCSRApproval()
			assertValidClientCertificate(addonv1beta1.CustomSigner, addOnName, getSecretName(addonv1beta1.CustomSigner, addOnName, newSignerName), expectedProxyURL)
		})

		It("should rotate addon client cert successfully", func() {
			assertSuccessClusterBootstrap(managedClusterName, hubKubeconfigSecret)
			assertSuccessAddOnBootstrap(addonv1beta1.KubeClient, "")

			secretName := getSecretName(addonv1beta1.KubeClient, addOnName, "")
			secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			By("Wait for cert rotation")
			assertSuccessCSRApproval()
			Eventually(func() bool {
				newSecret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				return !reflect.DeepEqual(secret.Data, newSecret.Data)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should stop addon client cert update if too frequent", func() {
			assertSuccessClusterBootstrap(managedClusterName, hubKubeconfigSecret)
			assertSuccessAddOnBootstrap(addonv1beta1.KubeClient, "")

			// update subject for 15 times
			for i := 1; i <= 15; i++ {
				currentIndex := i
				Eventually(func() error {
					addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					if len(addOn.Status.Registrations) == 0 {
						return fmt.Errorf("no registrations found")
					}
					addOn.Status.Registrations = []addonv1beta1.RegistrationConfig{
						{
							Type: addonv1beta1.KubeClient,
							KubeClient: &addonv1beta1.KubeClientConfig{
								Driver: "csr",
								Subject: addonv1beta1.KubeClientSubject{
									BaseSubject: addonv1beta1.BaseSubject{
										User: fmt.Sprintf("test-%d", currentIndex),
									},
								},
							},
						},
					}
					addOn.Status.Namespace = addOnName
					_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
				// sleep 1 second to ensure controller issue a new csr.
				time.Sleep(1 * time.Second)
			}

			By("CSR should not exceed 10")
			csrs, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s,%s=%s", clusterv1.ClusterNameLabelKey, managedClusterName, addonv1beta1.AddonLabelKey, addOnName),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(csrs.Items) >= 10).ShouldNot(BeFalse())

			Eventually(func() error {
				addOn, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
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

func getSecretName(registrationType addonv1beta1.RegistrationType, addOnName, signerName string) string {
	if registrationType == addonv1beta1.KubeClient {
		return fmt.Sprintf("%s-hub-kubeconfig", addOnName)
	}
	return fmt.Sprintf("%s-%s-client-cert", addOnName, strings.ReplaceAll(signerName, "/", "-"))
}
